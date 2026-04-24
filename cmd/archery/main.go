package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rjchien728/archery-cli/internal/client"
	"github.com/rjchien728/archery-cli/internal/config"
	"github.com/rjchien728/archery-cli/internal/meta"
	"github.com/rjchien728/archery-cli/internal/output"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type usageErr struct{ msg string }

func (e *usageErr) Error() string { return e.msg }
func usageError(s string) error   { return &usageErr{s} }

func main() {
	var (
		dbFlag       string
		endpointFlag string
		instanceFlag string
		usernameFlag string
		passwordFlag string
		schemaFlag   string
		sqlFlag      string
		fileFlag     string
		limitFlag    int
		maxColWidth  int
		formatCSV    bool
		formatJSON   bool
		formatExpand bool
		verbose      bool
	)

	rootCmd := &cobra.Command{
		Use:   "archery [<db>]",
		Short: "psql-style CLI for hhyo/Archery",
		Long: `archery runs SELECT queries and meta commands against an Archery instance.

Required configuration (env or flag):
  ARCHERY_URL       https://archery.example.com
  ARCHERY_INSTANCE  the instance name configured in Archery
  ARCHERY_USERNAME  login username
  ARCHERY_PASSWORD  login password

Optional:
  ARCHERY_ALIASES   comma-separated short=full pairs, e.g. prod=db_orders_prod,stg=db_orders_stg

SQL source precedence: -c > -f > stdin (when not a TTY).

Meta commands (passed via -c):
  \l            list databases
  \dn           list schemas
  \dt           list tables
  \d <table>    list columns of a table
  \?            this help`,
		Args:          cobra.MaximumNArgs(1),
		Version:       fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if endpointFlag != "" {
				cfg.Endpoint = endpointFlag
			}
			if instanceFlag != "" {
				cfg.Instance = instanceFlag
			}
			if usernameFlag != "" {
				cfg.Username = usernameFlag
			}
			if passwordFlag != "" {
				cfg.Password = passwordFlag
			}
			if err := cfg.Validate(); err != nil {
				return usageError(err.Error())
			}

			var dbInput string
			switch {
			case dbFlag != "" && len(args) > 0 && dbFlag != args[0]:
				return usageError("specify database via positional argument OR -d, not both")
			case dbFlag != "":
				dbInput = dbFlag
			case len(args) > 0:
				dbInput = args[0]
			default:
				return usageError("missing database (positional <db> or -d)")
			}

			fmtCount := b2i(formatCSV) + b2i(formatJSON) + b2i(formatExpand)
			if fmtCount > 1 {
				return usageError("--csv, --json and -x are mutually exclusive")
			}

			sql, err := readSQL(sqlFlag, fileFlag)
			if err != nil {
				return err
			}

			var verboseW io.Writer
			if verbose {
				verboseW = os.Stderr
			}
			opts := []client.Option{}
			if verboseW != nil {
				opts = append(opts, client.WithVerbose(verboseW))
			}
			c, err := client.New(cfg, opts...)
			if err != nil {
				return err
			}

			db, hit := cfg.ResolveDB(dbInput)
			if verbose {
				if hit {
					fmt.Fprintf(os.Stderr, "[archery] alias %s -> %s\n", dbInput, db)
				} else if len(cfg.AliasNames()) > 0 {
					fmt.Fprintf(os.Stderr, "[archery] db=%s (no alias hit; aliases: %s)\n", db, strings.Join(cfg.AliasNames(), ", "))
				}
			}

			if meta.IsMeta(sql) {
				res, handled, err := meta.Dispatch(sql, db, schemaFlag, c, os.Stdout)
				if err != nil {
					return err
				}
				if !handled || res == nil {
					return nil
				}
				return render(res.Columns, res.Rows, nil, formatCSV, formatJSON, formatExpand, maxColWidth)
			}

			qr, err := c.Query(db, schemaFlag, sql, limitFlag)
			if err != nil {
				return err
			}
			extras := map[string]any{
				"query_time":    qr.QueryTime,
				"affected_rows": qr.AffectedRows,
			}
			if qr.FullSQL != "" {
				extras["full_sql"] = qr.FullSQL
			}
			return render(qr.ColumnList, qr.Rows, extras, formatCSV, formatJSON, formatExpand, maxColWidth)
		},
	}

	f := rootCmd.Flags()
	f.StringVarP(&dbFlag, "database", "d", "", "database (alias or full name)")
	f.StringVar(&endpointFlag, "endpoint", "", "Archery URL (overrides ARCHERY_URL)")
	f.StringVar(&instanceFlag, "instance", "", "instance name (overrides ARCHERY_INSTANCE)")
	f.StringVar(&usernameFlag, "username", "", "username (overrides ARCHERY_USERNAME)")
	f.StringVar(&passwordFlag, "password", "", "password (overrides ARCHERY_PASSWORD)")
	f.StringVar(&schemaFlag, "schema", "public", "schema name")
	f.StringVarP(&sqlFlag, "command", "c", "", "SQL or meta command to execute")
	f.StringVarP(&fileFlag, "file", "f", "", "read SQL from file ('-' = stdin)")
	f.IntVarP(&limitFlag, "limit", "L", 100, "limit_num passed to archery")
	f.IntVar(&maxColWidth, "max-col-width", 60, "truncate cells wider than this (0 = no cap)")
	f.BoolVar(&formatCSV, "csv", false, "output CSV")
	f.BoolVar(&formatJSON, "json", false, "output JSON")
	f.BoolVarP(&formatExpand, "expanded", "x", false, "expanded display (one column per line)")
	f.BoolVarP(&verbose, "verbose", "v", false, "log progress to stderr")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "archery: "+err.Error())
		os.Exit(exitCodeFor(err))
	}
}

func readSQL(cmdFlag, fileFlag string) (string, error) {
	switch {
	case cmdFlag != "":
		return strings.TrimSpace(cmdFlag), nil
	case fileFlag == "-":
		return readAllString(os.Stdin)
	case fileFlag != "":
		b, err := os.ReadFile(fileFlag)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	default:
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return readAllString(os.Stdin)
		}
		return "", usageError("provide SQL via -c, -f, or piped stdin")
	}
}

func readAllString(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", usageError("empty SQL")
	}
	return s, nil
}

func render(cols []string, rows [][]any, extras map[string]any, csvOut, jsonOut, expanded bool, maxW int) error {
	switch {
	case csvOut:
		return output.CSV(os.Stdout, cols, rows)
	case jsonOut:
		return output.JSON(os.Stdout, cols, rows, extras)
	case expanded:
		return output.Expanded(os.Stdout, cols, rows, output.Options{MaxColWidth: maxW})
	default:
		return output.Table(os.Stdout, cols, rows, output.Options{MaxColWidth: maxW})
	}
}

func exitCodeFor(err error) int {
	var ue *usageErr
	if errors.As(err, &ue) {
		return 2
	}
	if errors.Is(err, client.ErrAuthFailed) {
		return 5
	}
	var se *client.ServerError
	if errors.As(err, &se) {
		return 1
	}
	msg := err.Error()
	if strings.Contains(msg, "network error") {
		return 3
	}
	if strings.Contains(msg, "server error HTTP") {
		return 4
	}
	return 1
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
