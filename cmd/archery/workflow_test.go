package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
			t.Setenv("ARCHERY_INSTANCE", "unused")
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
