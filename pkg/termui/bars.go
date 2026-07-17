// Package termui renders small ANSI terminal widgets (proportional bar
// charts, calendar heatmaps) with no dependency on any particular
// application's domain types.
package termui

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

// BarWidth is the number of characters a fully-filled bar occupies.
const BarWidth = 30

// BarItem is one labeled row in a proportional bar chart.
type BarItem struct {
	Label    string
	Fraction float64 // 0..1, how much of the bar is filled
	Detail   string  // optional trailing text, e.g. "(4/5)"
}

// PrintBars renders one proportional bar per item, colored red/yellow/green
// as Fraction crosses 0.5/0.8, with labels left-aligned to the longest one.
// If items is empty, empty is printed instead (e.g. "  (no attempts yet)").
func PrintBars(items []BarItem, empty string) {
	if len(items) == 0 {
		fmt.Println(empty)
		return
	}

	maxLen := 0
	for _, it := range items {
		if len(it.Label) > maxLen {
			maxLen = len(it.Label)
		}
	}

	for _, it := range items {
		filled := int(it.Fraction * BarWidth)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", BarWidth-filled)
		label := fmt.Sprintf("%-*s", maxLen, it.Label)
		pct := fmt.Sprintf("%3.0f%%", it.Fraction*100)
		barColor := color.New(color.FgGreen)
		if it.Fraction < 0.5 {
			barColor = color.New(color.FgRed)
		} else if it.Fraction < 0.8 {
			barColor = color.New(color.FgYellow)
		}
		line := fmt.Sprintf("  %s  %s  %s", label, barColor.Sprint(bar), pct)
		if it.Detail != "" {
			line += "  " + it.Detail
		}
		fmt.Println(line)
	}
}
