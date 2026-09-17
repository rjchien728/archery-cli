package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rjchien728/archery-cli/internal/client"
)

func TestParseWorkflowID(t *testing.T) {
	tests := []struct {
		desc    string
		in      string
		want    int
		wantErr bool
	}{
		{desc: "plain id", in: "896", want: 896},
		{desc: "zero is rejected", in: "0", wantErr: true},
		{desc: "negative is rejected", in: "-1", wantErr: true},
		{desc: "not a number is rejected", in: "latest", wantErr: true},
		{desc: "empty is rejected", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := parseWorkflowID(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, 2, exitCodeFor(err), "a bad id is a usage error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSQLRowsTable(t *testing.T) {
	cols, rows := sqlRowsTable([]client.SQLRow{
		{ID: 1, StageStatus: "Execute Successfully", AffectedRows: 3, ActualAffectedRows: 7, ExecuteTime: 0.002, ErrorMessage: "None", SQL: "UPDATE t SET a=1"},
	})
	assert.Equal(t, []string{"id", "stage_status", "affected_rows", "actual_affected_rows", "execute_time", "error_message", "sql"}, cols)
	require.Len(t, rows, 1)
	assert.Equal(t, []any{1, "Execute Successfully", 3, any(7), 0.002, "None", "UPDATE t SET a=1"}, rows[0])
}

func TestWorkflowCommandTree(t *testing.T) {
	cmd := newWorkflowCmd()
	got := map[string]bool{}
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, want := range []string{"check", "submit", "list", "show", "log", "approve", "cancel", "execute", "status"} {
		assert.True(t, got[want], "missing subcommand %q", want)
	}
}

// Every state-changing subcommand must fail before reaching archery when its
// arguments are wrong, so a bad invocation never half-runs.
func TestWorkflowArgValidationFailsOffline(t *testing.T) {
	tests := []struct {
		desc string
		args []string
		want string
	}{
		{desc: "approve without an id", args: []string{"approve"}, want: "accepts 1 arg"},
		{desc: "execute with a bad id", args: []string{"execute", "abc"}, want: "invalid workflow id"},
		{desc: "execute with an unknown mode", args: []string{"execute", "896", "--mode", "turbo"}, want: "invalid --mode"},
		{desc: "show with two ids", args: []string{"show", "1", "2"}, want: "accepts 1 arg"},
		{desc: "submit without --name", args: []string{"submit", "-d", "db_x", "-c", "SELECT 1;"}, want: "missing workflow name"},
		{desc: "check without -d", args: []string{"check", "-c", "SELECT 1;"}, want: "missing database"},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// These must all fail on their arguments. Pointing at a closed port
			// keeps a regression loud and fast instead of hanging on a real host.
			t.Setenv("ARCHERY_URL", "http://127.0.0.1:1")
			t.Setenv("ARCHERY_INSTANCE", "") // no command here resolves a target
			t.Setenv("ARCHERY_USERNAME", "unused")
			t.Setenv("ARCHERY_PASSWORD", "unused")

			cmd := newWorkflowCmd()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// check must exit non-zero when archery's audit reports errors, so a scripted
// `check && submit` gates on the result instead of always proceeding.
func TestCheckCommandExitCodeOnAuditResult(t *testing.T) {
	tests := []struct {
		desc    string
		body    string
		wantErr bool
	}{
		{desc: "errors fail", body: `{"error_count":1,"warning_count":0,"is_critical":false,"rows":[]}`, wantErr: true},
		{desc: "is_critical fails", body: `{"error_count":0,"warning_count":0,"is_critical":true,"rows":[]}`, wantErr: true},
		{desc: "warnings alone pass", body: `{"error_count":0,"warning_count":2,"is_critical":false,"rows":[]}`, wantErr: false},
		{
			// The shape archery actually returns when it rejects a statement:
			// execute_time is "" here but a number on accepted ones.
			desc: "a rejected statement fails, and its row still renders",
			body: `{"error_count":1,"warning_count":0,"is_critical":false,"rows":[{"id":1,"errlevel":2,
				"stagestatus":"驳回不支持语句","errormessage":"仅支持DML和DDL语句，查询语句请使用SQL查询功能！",
				"sql":"SELECT 1;","affected_rows":0,"execute_time":"","actual_affected_rows":""}]}`,
			wantErr: true,
		},
		{desc: "clean audit passes", body: `{"error_count":0,"warning_count":0,"is_critical":false,"rows":[]}`, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v1/workflow/sqlcheck/", r.URL.Path)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			// HOME points at a temp dir so the cookie cache stays out of the real
			// home. Numeric --instance/--group skip name resolution, so sqlcheck is
			// the only endpoint touched.
			t.Setenv("HOME", t.TempDir())
			t.Setenv("ARCHERY_URL", srv.URL)
			t.Setenv("ARCHERY_INSTANCE", "20")
			t.Setenv("ARCHERY_USERNAME", "u")
			t.Setenv("ARCHERY_PASSWORD", "p")

			cmd := newWorkflowCmd()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"check", "--instance", "20", "--group", "6", "-d", "db", "-c", "UPDATE t SET a=1"})
			err := cmd.Execute()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "audit failed")
				assert.Equal(t, 1, exitCodeFor(err), "a failed audit is a generic failure")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWorkflowFlagsRejectMultipleFormats(t *testing.T) {
	wf := &workflowFlags{formatCSV: true, formatJSON: true}
	err := wf.checkFormats()
	require.Error(t, err)
	assert.Equal(t, 2, exitCodeFor(err))
}

// The root command keeps taking a database positionally; adding `workflow`
// must not have turned that into a subcommand lookup failure.
func TestRootStillAcceptsPositionalDatabase(t *testing.T) {
	root := &cobra.Command{Use: "archery [<db>]", Args: cobra.MaximumNArgs(1)}
	root.AddCommand(newWorkflowCmd())
	root.SetArgs([]string{"chat-dev"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	assert.NoError(t, root.Execute(), "a db name that is not a subcommand must still reach the root command")
}

// The lazy password prompt only pays off if cmd actually hands the client its
// callback. Without this, dropping passwordPrompt() from the option lists
// compiles, passes every other test, and silently sends an empty password.
func TestPasswordPromptWiring(t *testing.T) {
	tests := []struct {
		desc        string
		cachedLogin bool
		wantPrompts int
	}{
		{desc: "a valid cached session never asks for a password", cachedLogin: true, wantPrompts: 0},
		{desc: "no session asks once, via the injected callback", cachedLogin: false, wantPrompts: 1},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			authenticated := tt.cachedLogin
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/login/":
					http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "t", Path: "/"})
				case "/authenticate/":
					assert.Equal(t, "prompted-pw", r.FormValue("password"), "the password must come from the callback")
					authenticated = true
					http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "s", Path: "/"})
					_, _ = io.WriteString(w, `{"status":0,"msg":"ok","data":null}`)
				case "/api/v1/workflow/sqlcheck/":
					if !authenticated {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					_, _ = io.WriteString(w, `{"error_count":0,"warning_count":0,"is_critical":false,"rows":[]}`)
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
			}))
			defer srv.Close()

			home := t.TempDir()
			if tt.cachedLogin {
				writeCookieCache(t, home)
			}
			t.Setenv("HOME", home)
			t.Setenv("ARCHERY_URL", srv.URL)
			t.Setenv("ARCHERY_INSTANCE", "20")
			t.Setenv("ARCHERY_USERNAME", "u")
			t.Setenv("ARCHERY_PASSWORD", "") // the callback is the only source

			prompts := 0
			original := promptPassword
			promptPassword = func() (string, error) { prompts++; return "prompted-pw", nil }
			t.Cleanup(func() { promptPassword = original })

			cmd := newWorkflowCmd()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"check", "--instance", "20", "--group", "6", "-d", "db", "-c", "UPDATE t SET a=1"})
			require.NoError(t, cmd.Execute())

			assert.Equal(t, tt.wantPrompts, prompts)
		})
	}
}

// writeCookieCache seeds the on-disk session the client loads at startup, in the
// shape saveCookies writes.
func writeCookieCache(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".cache", "archery")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	cookies := []map[string]any{
		{"name": "sessionid", "value": "s", "path": "/", "expires": time.Now().Add(time.Hour)},
		{"name": "csrftoken", "value": "t", "path": "/"},
	}
	blob, err := json.Marshal(cookies)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cookies.json"), blob, 0o600))
}

// A subcommand must advertise only the flags it reads. The id-addressed ones act
// on a workflow that already exists, so naming a target means nothing to them —
// and a flag that is accepted but ignored is worse than one that is rejected.
func TestSubcommandsAdvertiseOnlyTheFlagsTheyRead(t *testing.T) {
	targetFlags := []string{"instance", "group"}
	sqlFlags := []string{"database", "command", "file"}

	want := map[string]struct{ target, sql bool }{
		"check":   {target: true, sql: true},
		"submit":  {target: true, sql: true},
		"list":    {target: true, sql: false},
		"show":    {target: false, sql: false},
		"log":     {target: false, sql: false},
		"status":  {target: false, sql: false},
		"approve": {target: false, sql: false},
		"cancel":  {target: false, sql: false},
		"execute": {target: false, sql: false},
	}

	root := newWorkflowCmd()
	for _, sub := range root.Commands() {
		expect, known := want[sub.Name()]
		require.True(t, known, "unlisted subcommand %q — add it to this table", sub.Name())

		t.Run(sub.Name(), func(t *testing.T) {
			has := func(name string) bool {
				return sub.Flags().Lookup(name) != nil || sub.InheritedFlags().Lookup(name) != nil
			}
			for _, f := range targetFlags {
				assert.Equal(t, expect.target, has(f), "--%s on %q", f, sub.Name())
			}
			for _, f := range sqlFlags {
				assert.Equal(t, expect.sql, has(f), "--%s on %q", f, sub.Name())
			}
			// Connection and output flags stay shared by all of them.
			for _, f := range []string{"endpoint", "username", "json", "csv", "verbose"} {
				assert.True(t, has(f), "--%s must stay shared, missing on %q", f, sub.Name())
			}
		})
	}
}

// Commands addressed by a workflow id never resolve an instance, so they must
// run without ARCHERY_INSTANCE. Commands that name a target must say so plainly.
func TestInstanceIsRequiredOnlyWhereItIsUsed(t *testing.T) {
	t.Run("approve works with no instance configured", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/passed/", r.URL.Path)
			w.Header().Set("Location", "/detail/900/")
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()

		t.Setenv("HOME", t.TempDir())
		t.Setenv("ARCHERY_URL", srv.URL)
		t.Setenv("ARCHERY_INSTANCE", "")
		t.Setenv("ARCHERY_USERNAME", "u")
		t.Setenv("ARCHERY_PASSWORD", "p")

		cmd := newWorkflowCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"approve", "900"})
		require.NoError(t, cmd.Execute())
	})

	t.Run("check without an instance is a usage error", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ARCHERY_URL", "http://127.0.0.1:1")
		t.Setenv("ARCHERY_INSTANCE", "")
		t.Setenv("ARCHERY_USERNAME", "u")
		t.Setenv("ARCHERY_PASSWORD", "p")

		cmd := newWorkflowCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"check", "-d", "db", "-c", "UPDATE t SET a=1"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing instance")
		assert.Equal(t, 2, exitCodeFor(err))
	})
}
