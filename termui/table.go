package termui

import (
	"fmt"
	"strings"
)

// PrintTable renders header and rows as a left-aligned, space-padded table,
// with each column's width computed from its widest cell (header included).
// Every row must have the same number of cells as header.
func PrintTable(header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	printRow := func(cells []string) {
		parts := make([]string, len(cells))
		for i, cell := range cells {
			if i == len(cells)-1 {
				parts[i] = cell // last column: no trailing padding needed
				continue
			}
			parts[i] = fmt.Sprintf("%-*s", widths[i], cell)
		}
		fmt.Println(strings.Join(parts, "  "))
	}

	printRow(header)
	for _, row := range rows {
		printRow(row)
	}
}
