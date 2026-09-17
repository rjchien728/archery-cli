package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rjchien728/archery-cli/internal/client"
)

// workflowFlags are shared by every `archery workflow` subcommand. They mirror
// the root command's connection flags; the query-only ones (--schema, --limit)
// are deliberately absent.
type workflowFlags struct {
	endpoint     string
	instance     string
	username     string
	group        string
	database     string
	sql          string
	file         string
	insecure     bool
	cacert       string
	verbose      bool
	formatCSV    bool
	formatJSON   bool
	formatExpand bool
	maxColWidth  int
}

func newWorkflowCmd() *cobra.Command {
	wf := &workflowFlags{}

	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "submit, review and execute archery SQL workflows",
		Long: `workflow drives archery's SQL review flow: submit a workflow, approve it,
then execute it. Each subcommand maps to exactly one action in archery's web UI;
combining them (submit, then approve, then execute) is left to the caller.

Approving a workflow does not run it. execute is a separate step, as in the UI.

--instance and --group both accept either a name or a numeric id. Names are
resolved by reading the groups you may submit to and the instances inside them;
passing ids skips that lookup entirely.

Because this is a subcommand, a database literally named "workflow" has to be
passed as -d workflow.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	p := cmd.PersistentFlags()
	p.StringVar(&wf.endpoint, "endpoint", "", "Archery URL (overrides ARCHERY_URL)")
	p.StringVar(&wf.instance, "instance", "", "instance name or id (overrides ARCHERY_INSTANCE)")
	p.StringVar(&wf.username, "username", "", "username (overrides ARCHERY_USERNAME)")
	p.StringVar(&wf.group, "group", "", "resource group name or id (default: the only group holding the instance)")
	p.StringVarP(&wf.database, "database", "d", "", "database (alias or full name)")
	p.StringVarP(&wf.sql, "command", "c", "", "SQL to submit or check")
	p.StringVarP(&wf.file, "file", "f", "", "read SQL from file ('-' = stdin)")
	p.BoolVarP(&wf.insecure, "insecure", "k", false, "skip TLS certificate verification (unsafe; for MITM-free internal networks only)")
	p.StringVar(&wf.cacert, "cacert", "", "path to PEM file with extra trusted CA certificates")
	p.BoolVarP(&wf.verbose, "verbose", "v", false, "log progress to stderr")
	p.BoolVar(&wf.formatCSV, "csv", false, "output CSV")
	p.BoolVar(&wf.formatJSON, "json", false, "output JSON")
	p.BoolVarP(&wf.formatExpand, "expanded", "x", false, "expanded display (one column per line)")
	p.IntVar(&wf.maxColWidth, "max-col-width", 60, "truncate cells wider than this (0 = no cap)")

	cmd.AddCommand(
		newWorkflowCheckCmd(wf),
		newWorkflowSubmitCmd(wf),
		newWorkflowListCmd(wf),
		newWorkflowShowCmd(wf),
		newWorkflowLogCmd(wf),
		newWorkflowApproveCmd(wf),
		newWorkflowCancelCmd(wf),
		newWorkflowExecuteCmd(wf),
		newWorkflowStatusCmd(wf),
	)
	return cmd
}

// connect builds a client from the workflow flags plus the environment, and
// resolves -d through the configured aliases the same way the query path does.
func (wf *workflowFlags) connect() (*client.Client, string, error) {
	cfg, err := resolveConfig(wf.endpoint, wf.instance, wf.username, wf.cacert, wf.insecure)
	if err != nil {
		return nil, "", err
	}
	opts := []client.Option{}
	if wf.verbose {
		opts = append(opts, client.WithVerbose(os.Stderr))
	}
	c, err := client.New(cfg, opts...)
	if err != nil {
		return nil, "", err
	}
	db, _ := cfg.ResolveDB(wf.database)
	return c, db, nil
}

// target resolves the instance/group pair the workflow endpoints need. The
// instance is whatever --instance or ARCHERY_INSTANCE settled on.
func (wf *workflowFlags) target(c *client.Client) (*client.Target, error) {
	return c.ResolveTarget(c.Instance(), wf.group)
}

func (wf *workflowFlags) checkFormats() error {
	if b2i(wf.formatCSV)+b2i(wf.formatJSON)+b2i(wf.formatExpand) > 1 {
		return usageError("--csv, --json and -x are mutually exclusive")
	}
	return nil
}

func (wf *workflowFlags) render(cols []string, rows [][]any, extras map[string]any) error {
	return render(cols, rows, extras, wf.formatCSV, wf.formatJSON, wf.formatExpand, wf.maxColWidth)
}

func (wf *workflowFlags) requireDB() error {
	if wf.database == "" {
		return usageError("missing database (-d)")
	}
	return nil
}

func sqlRowsTable(rows []client.SQLRow) ([]string, [][]any) {
	// affected_rows is archery's pre-execution estimate; actual_affected_rows is
	// what the statement really touched and is empty until it runs.
	cols := []string{"id", "stage_status", "affected_rows", "actual_affected_rows", "execute_time", "error_message", "sql"}
	out := make([][]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, []any{r.ID, r.StageStatus, r.AffectedRows, r.ActualAffectedRows, r.ExecuteTime, r.ErrorMessage, r.SQL})
	}
	return cols, out
}

func newWorkflowCheckCmd(wf *workflowFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "run archery's SQL audit without creating a workflow",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := wf.checkFormats(); err != nil {
				return err
			}
			if err := wf.requireDB(); err != nil {
				return err
			}
			sql, err := readSQL(wf.sql, wf.file)
			if err != nil {
				return err
			}
			c, db, err := wf.connect()
			if err != nil {
				return err
			}
			tgt, err := wf.target(c)
			if err != nil {
				return err
			}
			res, err := c.Check(tgt.InstanceID, db, sql)
			if err != nil {
				return err
			}
			cols, rows := sqlRowsTable(res.Rows)
			return wf.render(cols, rows, map[string]any{
				"error_count":   res.ErrorCount,
				"warning_count": res.WarningCount,
				"syntax_type":   res.SyntaxType,
				"is_critical":   res.IsCritical,
			})
		},
	}
}

func newWorkflowSubmitCmd(wf *workflowFlags) *cobra.Command {
	var (
		name         string
		backup       bool
		demandURL    string
		runDateStart string
		runDateEnd   string
	)
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "create a SQL workflow (lands in manual review)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := wf.checkFormats(); err != nil {
				return err
			}
			if err := wf.requireDB(); err != nil {
				return err
			}
			if strings.TrimSpace(name) == "" {
				return usageError("missing workflow name (--name)")
			}
			sql, err := readSQL(wf.sql, wf.file)
			if err != nil {
				return err
			}
			c, db, err := wf.connect()
			if err != nil {
				return err
			}
			tgt, err := wf.target(c)
			if err != nil {
				return err
			}
			res, err := c.Submit(client.SubmitRequest{
				Name:         name,
				GroupID:      tgt.GroupID,
				InstanceID:   tgt.InstanceID,
				DBName:       db,
				SQL:          sql,
				IsBackup:     backup,
				DemandURL:    demandURL,
				RunDateStart: runDateStart,
				RunDateEnd:   runDateEnd,
			})
			if err != nil {
				return err
			}
			cols := []string{"workflow_id", "status", "group", "db", "audit_auth_groups"}
			rows := [][]any{{res.WorkflowID, res.Workflow.Status, res.Workflow.GroupName, res.Workflow.DBName, res.Workflow.AuditAuthGroups}}
			return wf.render(cols, rows, nil)
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "workflow name (required)")
	f.BoolVar(&backup, "backup", false, "ask archery to back up affected rows")
	f.StringVar(&demandURL, "demand-url", "", "link to the ticket this change comes from")
	f.StringVar(&runDateStart, "run-date-start", "", "start of the executable window (YYYY-MM-DD HH:MM:SS)")
	f.StringVar(&runDateEnd, "run-date-end", "", "end of the executable window (YYYY-MM-DD HH:MM:SS)")
	return cmd
}

func newWorkflowListCmd(wf *workflowFlags) *cobra.Command {
	var (
		status      string
		syntaxTypes []int
		since       string
		until       string
		search      string
		limit       int
		offset      int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list workflows visible to you",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := wf.checkFormats(); err != nil {
				return err
			}
			c, _, err := wf.connect()
			if err != nil {
				return err
			}
			opts := client.ListOptions{
				Status:      status,
				SyntaxTypes: syntaxTypes,
				StartDate:   since,
				EndDate:     until,
				Search:      search,
				Limit:       limit,
				Offset:      offset,
			}
			// Both filters are optional and independent: resolving a group alone
			// must not drag in ARCHERY_INSTANCE, which is unrelated to what the
			// caller asked to list.
			switch {
			case wf.instance != "":
				tgt, err := wf.target(c)
				if err != nil {
					return err
				}
				opts.GroupID = tgt.GroupID
				opts.InstanceID = tgt.InstanceID
			case wf.group != "":
				groupID, err := c.GroupID(wf.group)
				if err != nil {
					return err
				}
				opts.GroupID = groupID
			}
			res, err := c.List(opts)
			if err != nil {
				return err
			}
			cols := []string{"id", "workflow_name", "status", "engineer", "group", "instance", "db", "create_time"}
			rows := make([][]any, 0, len(res.Rows))
			for _, r := range res.Rows {
				rows = append(rows, []any{r.ID, r.WorkflowName, r.Status, r.Engineer, r.GroupName, r.InstanceName, r.DBName, r.CreateTime})
			}
			return wf.render(cols, rows, map[string]any{"total": res.Total})
		},
	}
	f := cmd.Flags()
	f.StringVar(&status, "status", "", "filter by status (e.g. workflow_manreviewing, workflow_finish)")
	f.IntSliceVar(&syntaxTypes, "syntax-type", nil, "filter by syntax type (1=DDL, 2=DML; default: all)")
	f.StringVar(&since, "since", "", "earliest create date (YYYY-MM-DD)")
	f.StringVar(&until, "until", "", "latest create date (YYYY-MM-DD)")
	f.StringVar(&search, "search", "", "search workflow names")
	f.IntVar(&limit, "limit", 20, "page size")
	f.IntVar(&offset, "offset", 0, "page offset")
	return cmd
}

func newWorkflowShowCmd(wf *workflowFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "show <workflow-id>",
		Short: "show a workflow's per-statement audit and execution result",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := wf.checkFormats(); err != nil {
				return err
			}
			id, err := parseWorkflowID(args[0])
			if err != nil {
				return err
			}
			c, _, err := wf.connect()
			if err != nil {
				return err
			}
			detail, err := c.Detail(id)
			if err != nil {
				return err
			}
			cols, rows := sqlRowsTable(detail)
			return wf.render(cols, rows, nil)
		},
	}
}

func newWorkflowLogCmd(wf *workflowFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "log <workflow-id>",
		Short: "show a workflow's audit trail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := wf.checkFormats(); err != nil {
				return err
			}
			id, err := parseWorkflowID(args[0])
			if err != nil {
				return err
			}
			c, _, err := wf.connect()
			if err != nil {
				return err
			}
			entries, err := c.Log(id)
			if err != nil {
				return err
			}
			cols := []string{"operation_time", "operation_type", "operator", "operation_info"}
			rows := make([][]any, 0, len(entries))
			for _, e := range entries {
				rows = append(rows, []any{e.OperationTime, e.OperationType, e.Operator, e.OperationInfo})
			}
			return wf.render(cols, rows, nil)
		},
	}
}

func newWorkflowApproveCmd(wf *workflowFlags) *cobra.Command {
	var remark string
	cmd := &cobra.Command{
		Use:   "approve <workflow-id>",
		Short: "pass a workflow's manual review (does not execute it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return wf.act(args[0], "approve", func(c *client.Client, id int) error {
				return c.Approve(id, remark)
			})
		},
	}
	cmd.Flags().StringVar(&remark, "remark", "", "review note recorded on the workflow")
	return cmd
}

func newWorkflowCancelCmd(wf *workflowFlags) *cobra.Command {
	var remark string
	cmd := &cobra.Command{
		Use:   "cancel <workflow-id>",
		Short: "reject a pending workflow or abort your own",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return wf.act(args[0], "cancel", func(c *client.Client, id int) error {
				return c.Cancel(id, remark)
			})
		},
	}
	cmd.Flags().StringVar(&remark, "remark", "", "reason for cancelling, recorded on the workflow")
	return cmd
}

func newWorkflowExecuteCmd(wf *workflowFlags) *cobra.Command {
	var mode string
	cmd := &cobra.Command{
		Use:   "execute <workflow-id>",
		Short: "run an approved workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode != "auto" && mode != "manual" {
				return usageError(fmt.Sprintf("invalid --mode %q (want auto or manual)", mode))
			}
			return wf.act(args[0], "execute", func(c *client.Client, id int) error {
				return c.Execute(id, mode)
			})
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "auto", "auto (archery runs it) or manual (record that you ran it by hand)")
	return cmd
}

func newWorkflowStatusCmd(wf *workflowFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status <workflow-id>",
		Short: "print a workflow's current status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := wf.checkFormats(); err != nil {
				return err
			}
			id, err := parseWorkflowID(args[0])
			if err != nil {
				return err
			}
			c, _, err := wf.connect()
			if err != nil {
				return err
			}
			status, err := c.Status(id)
			if err != nil {
				return err
			}
			return wf.render([]string{"workflow_id", "status"}, [][]any{{id, status}}, nil)
		},
	}
}

// act runs one of the state-changing form endpoints and reports what was done.
func (wf *workflowFlags) act(rawID, action string, do func(*client.Client, int) error) error {
	if err := wf.checkFormats(); err != nil {
		return err
	}
	id, err := parseWorkflowID(rawID)
	if err != nil {
		return err
	}
	c, _, err := wf.connect()
	if err != nil {
		return err
	}
	if err := do(c, id); err != nil {
		return err
	}
	return wf.render([]string{"workflow_id", "action"}, [][]any{{id, action}}, nil)
}

func parseWorkflowID(s string) (int, error) {
	id, err := strconv.Atoi(s)
	if err != nil || id <= 0 {
		return 0, usageError(fmt.Sprintf("invalid workflow id %q", s))
	}
	return id, nil
}
