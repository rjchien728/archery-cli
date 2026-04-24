package output

import (
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"io"
)

// CSV writes RFC 4180 CSV with header row.
func CSV(w io.Writer, cols []string, rows [][]any) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(cols); err != nil {
		return err
	}
	for _, r := range rows {
		out := make([]string, len(cols))
		for j := 0; j < len(cols); j++ {
			var v any
			if j < len(r) {
				v = r[j]
			}
			out[j] = renderCellCSV(v)
		}
		if err := cw.Write(out); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func renderCellCSV(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(x)
	}
}
