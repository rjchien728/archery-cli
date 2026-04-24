package meta

import (
	"fmt"
	"io"
	"strings"

	"github.com/rjchien728/archery-cli/internal/client"
)

// Result mirrors a query result so the caller can render it through the
// standard output formatters.
type Result struct {
	Columns []string
	Rows    [][]any
}

// HelpText is the cheatsheet printed by \?.
const HelpText = `Meta commands:
  \l            list databases on the configured instance
  \dn           list schemas in the current database
  \dt           list tables in the current schema
  \d <table>    list columns of a table
  \?            print this help
`

// IsMeta reports whether a string looks like a meta command (starts with '\').
func IsMeta(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), `\`)
}

// Dispatch executes a meta command.
//
// - (result, true, nil): meta command produced a rendered result.
// - (nil,    true, nil): meta command was handled internally (e.g. \?).
// - (nil,    false, nil): not a meta command, caller should treat as SQL.
// - (nil,    true, err): meta command was recognized but failed.
func Dispatch(input string, db, schema string, c *client.Client, out io.Writer) (*Result, bool, error) {
	s := strings.TrimSpace(input)
	if !strings.HasPrefix(s, `\`) {
		return nil, false, nil
	}
	// Strip optional trailing semicolon (psql convention).
	s = strings.TrimSuffix(s, ";")
	fields := strings.Fields(s)
	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case `\?`, `\h`, `\help`:
		fmt.Fprint(out, HelpText)
		return nil, true, nil
	case `\l`:
		names, err := c.InstanceResource(client.ResDatabase, "", "", "")
		if err != nil {
			return nil, true, err
		}
		return listResult("database", names), true, nil
	case `\dn`:
		names, err := c.InstanceResource(client.ResSchema, db, "", "")
		if err != nil {
			return nil, true, err
		}
		return listResult("schema", names), true, nil
	case `\dt`:
		names, err := c.InstanceResource(client.ResTable, db, schema, "")
		if err != nil {
			return nil, true, err
		}
		return listResult("table", names), true, nil
	case `\d`:
		if len(args) == 0 {
			return nil, true, fmt.Errorf(`\d requires a table name (e.g. \d users)`)
		}
		names, err := c.InstanceResource(client.ResColumn, db, schema, args[0])
		if err != nil {
			return nil, true, err
		}
		return listResult("column", names), true, nil
	default:
		return nil, true, fmt.Errorf("unknown meta command: %s (try \\?)", cmd)
	}
}

func listResult(colName string, names []string) *Result {
	rows := make([][]any, len(names))
	for i, n := range names {
		rows[i] = []any{n}
	}
	return &Result{Columns: []string{colName}, Rows: rows}
}
