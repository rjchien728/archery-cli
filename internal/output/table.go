package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Table writes an aligned, psql-style table.
func Table(w io.Writer, cols []string, rows [][]any, opts Options) error {
	n := len(cols)
	if n == 0 {
		fmt.Fprintln(w, "(no columns)")
		return nil
	}
	rendered := make([][]string, len(rows))
	for i, r := range rows {
		row := make([]string, n)
		for j := 0; j < n; j++ {
			var v any
			if j < len(r) {
				v = r[j]
			}
			row[j] = renderCell(v)
		}
		rendered[i] = row
	}
	widths := make([]int, n)
	for j, c := range cols {
		widths[j] = runewidth.StringWidth(c)
	}
	for _, row := range rendered {
		for j, c := range row {
			if cw := runewidth.StringWidth(c); cw > widths[j] {
				widths[j] = cw
			}
		}
	}
	if opts.MaxColWidth > 0 {
		for j := range widths {
			if widths[j] > opts.MaxColWidth {
				widths[j] = opts.MaxColWidth
			}
		}
	}
	writeRow(w, cols, widths)
	sepParts := make([]string, n)
	for j := range sepParts {
		sepParts[j] = strings.Repeat("-", widths[j]+2)
	}
	fmt.Fprintln(w, strings.Join(sepParts, "+"))
	for _, row := range rendered {
		writeRow(w, row, widths)
	}
	fmt.Fprintf(w, "(%d %s)\n", len(rows), pluralRows(len(rows)))
	return nil
}

func writeRow(w io.Writer, cells []string, widths []int) {
	parts := make([]string, len(widths))
	for j := 0; j < len(widths); j++ {
		var c string
		if j < len(cells) {
			c = cells[j]
		}
		c = truncateDisplay(c, widths[j])
		pad := widths[j] - runewidth.StringWidth(c)
		if pad < 0 {
			pad = 0
		}
		parts[j] = " " + c + strings.Repeat(" ", pad) + " "
	}
	fmt.Fprintln(w, strings.Join(parts, "|"))
}

func pluralRows(n int) string {
	if n == 1 {
		return "row"
	}
	return "rows"
}
