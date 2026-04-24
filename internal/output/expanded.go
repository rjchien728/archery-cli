package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Expanded writes one record per block, like psql's \x.
func Expanded(w io.Writer, cols []string, rows [][]any, opts Options) error {
	if len(cols) == 0 {
		fmt.Fprintln(w, "(no columns)")
		return nil
	}
	nameWidth := 0
	for _, c := range cols {
		if cw := runewidth.StringWidth(c); cw > nameWidth {
			nameWidth = cw
		}
	}
	for i, row := range rows {
		fmt.Fprintf(w, "-[ RECORD %d ]%s\n", i+1, strings.Repeat("-", nameWidth-1))
		for j, c := range cols {
			var v any
			if j < len(row) {
				v = row[j]
			}
			cell := renderCell(v)
			if opts.MaxColWidth > 0 {
				cell = truncateDisplay(cell, opts.MaxColWidth)
			}
			pad := nameWidth - runewidth.StringWidth(c)
			if pad < 0 {
				pad = 0
			}
			fmt.Fprintf(w, "%s%s | %s\n", c, strings.Repeat(" ", pad), cell)
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "(0 rows)")
	}
	return nil
}
