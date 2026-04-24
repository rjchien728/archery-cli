package output

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Options control the formatting of table / expanded output.
type Options struct {
	// MaxColWidth caps the display width of any single cell. 0 means unlimited.
	MaxColWidth int
}

// renderCell converts a value into its on-screen string representation for
// table / expanded output. Newlines are replaced with ↵; binaries are summarised.
func renderCell(v any) string {
	if v == nil {
		return "NULL"
	}
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, "\n", "↵")
	case []byte:
		return fmt.Sprintf("<binary %d bytes>", len(x))
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	case bool:
		if x {
			return "t"
		}
		return "f"
	default:
		return strings.ReplaceAll(fmt.Sprint(x), "\n", "↵")
	}
}

// truncateDisplay returns s capped at max display cells, appending … on truncation.
func truncateDisplay(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= max {
		return s
	}
	return runewidth.Truncate(s, max, "…")
}
