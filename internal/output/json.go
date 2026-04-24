package output

import (
	"encoding/json"
	"io"
)

// JSON writes a structured payload designed for downstream tooling: columns,
// rows, row_count, plus any extras the caller wants merged in (e.g. query_time).
func JSON(w io.Writer, cols []string, rows [][]any, extras map[string]any) error {
	out := map[string]any{
		"columns":   cols,
		"rows":      rows,
		"row_count": len(rows),
	}
	for k, v := range extras {
		out[k] = v
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
