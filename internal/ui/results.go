package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/backend"
	"github.com/diegopacheco/dev-cli/internal/syntax"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

const maxCellWidth = 48

func colored(color, text string) string {
	return "[" + color + "]" + tview.Escape(text) + "[-]"
}

func RenderResults(results []backend.Result, err error, asJSON bool) string {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(RenderResult(r, asJSON, true))
	}
	if err != nil {
		b.WriteString("\n" + colored(theme.Red, "✖ "+err.Error()) + "\n")
	}
	return b.String()
}

func RenderResult(r backend.Result, asJSON, withHeader bool) string {
	var b strings.Builder
	renderResult(&b, r, asJSON, withHeader)
	return b.String()
}

func renderResult(b *strings.Builder, r backend.Result, asJSON, withHeader bool) {
	header := colored(theme.Magenta, "▶ ") + colored(theme.Text, r.Title)
	if r.Message != "" {
		header += colored(theme.Dim, "  · ") + colored(theme.Lime, r.Message)
	}
	if r.Elapsed > 0 {
		header += colored(theme.Dim, "  · "+Duration(r.Elapsed))
	}
	if withHeader {
		b.WriteString(header + "\n")
	}
	switch {
	case r.Text != "" && !asJSON:
		b.WriteString(r.Text)
	case !withHeader && r.Columns == nil && r.Value == nil && len(r.Logs) == 0:
		b.WriteString(r.Message + "\n")
	case asJSON && r.Value != nil:
		b.WriteString(syntax.Pretty(r.Value) + "\n")
	case !asJSON && len(r.Logs) > 0:
		renderLogs(b, r.Logs)
	case asJSON && r.Columns != nil:
		b.WriteString(syntax.Pretty(rowsAsObjects(r)) + "\n")
	case r.Columns != nil:
		b.WriteString(Table(r.Columns, r.Rows))
	case r.Value != nil:
		b.WriteString(syntax.Pretty(r.Value) + "\n")
	}
}

func rowsAsObjects(r backend.Result) []any {
	out := make([]any, 0, len(r.Rows))
	for _, row := range r.Rows {
		obj := syntax.Object{}
		for i, c := range r.Columns {
			if i < len(row) {
				obj = append(obj, syntax.Pair{Key: c, Value: row[i]})
			}
		}
		out = append(out, obj)
	}
	return out
}

func truncate(s string, width int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", "⏎"), "\t", " ")
	if runewidth.StringWidth(s) <= width {
		return s
	}
	return runewidth.Truncate(s, width, "…")
}

func cellColor(v any) string {
	switch x := syntax.Normalize(v, false).(type) {
	case nil:
		return theme.Dim
	case json.Number:
		return theme.Orange
	case bool:
		return theme.Magenta
	case string:
		if _, ok := syntax.ParseEmbedded(x); ok {
			return theme.Purple
		}
		return theme.Text
	}
	return theme.Purple
}

func Table(columns []string, rows [][]any) string {
	widths := make([]int, len(columns))
	cells := make([][]string, len(rows))
	for i, c := range columns {
		widths[i] = min(maxCellWidth, runewidth.StringWidth(c))
	}
	for r, row := range rows {
		cells[r] = make([]string, len(columns))
		for i := range columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			text := truncate(syntax.Scalar(v), maxCellWidth)
			cells[r][i] = text
			widths[i] = max(widths[i], runewidth.StringWidth(text))
		}
	}
	line := func(left, mid, right string) string {
		parts := make([]string, len(widths))
		for i, w := range widths {
			parts[i] = strings.Repeat("─", w+2)
		}
		return colored(theme.Border, left+strings.Join(parts, mid)+right) + "\n"
	}
	pad := func(s string, w int) string {
		return s + strings.Repeat(" ", max(0, w-runewidth.StringWidth(s)))
	}
	sep := colored(theme.Border, "│")
	var b strings.Builder
	b.WriteString(line("┌", "┬", "┐"))
	b.WriteString(sep)
	for i, c := range columns {
		b.WriteString(" [" + theme.Cyan + "::b]" + tview.Escape(pad(truncate(c, maxCellWidth), widths[i])) + "[-::-] " + sep)
	}
	b.WriteString("\n")
	b.WriteString(line("├", "┼", "┤"))
	for r, row := range cells {
		b.WriteString(sep)
		for i, text := range row {
			var v any
			if i < len(rows[r]) {
				v = rows[r][i]
			}
			b.WriteString(" " + colored(cellColor(v), pad(text, widths[i])) + " " + sep)
		}
		b.WriteString("\n")
	}
	b.WriteString(line("└", "┴", "┘"))
	return b.String()
}

func MaskTarget(target string) string {
	at := strings.LastIndex(target, "@")
	scheme := strings.Index(target, "://")
	if at < 0 || scheme < 0 || at < scheme {
		return target
	}
	userinfo := target[scheme+3 : at]
	user, _, hasPass := strings.Cut(userinfo, ":")
	if !hasPass {
		return target
	}
	return fmt.Sprintf("%s%s:***%s", target[:scheme+3], user, target[at:])
}

func Duration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return d.Round(time.Microsecond).String()
	case d < time.Second:
		return d.Round(100 * time.Microsecond).String()
	}
	return d.Round(time.Millisecond).String()
}

func levelOf(labels syntax.Object) string {
	for _, key := range []string{"level", "detected_level", "severity"} {
		for _, p := range labels {
			if p.Key == key {
				return strings.ToUpper(syntax.Scalar(p.Value))
			}
		}
	}
	return ""
}

func levelColor(level string) string {
	switch {
	case strings.HasPrefix(level, "ERR"), strings.HasPrefix(level, "FATAL"), strings.HasPrefix(level, "CRIT"):
		return theme.Red
	case strings.HasPrefix(level, "WARN"):
		return theme.Orange
	case strings.HasPrefix(level, "INFO"):
		return theme.Lime
	case strings.HasPrefix(level, "DEBUG"), strings.HasPrefix(level, "TRACE"):
		return theme.Blue
	}
	return theme.Dim
}

func renderLogs(b *strings.Builder, logs []backend.LogLine) {
	for _, l := range logs {
		level := levelOf(l.Labels)
		b.WriteString(colored(theme.Dim, l.Time[11:]) + " ")
		b.WriteString(colored(levelColor(level), fmt.Sprintf("%-5s", truncate(level, 5))) + " ")
		var labels []string
		for _, p := range l.Labels {
			if p.Key == "level" || p.Key == "detected_level" || p.Key == "service_name" {
				continue
			}
			labels = append(labels, p.Key+"="+syntax.Scalar(p.Value))
		}
		b.WriteString(colored(theme.Purple, strings.Join(labels, " ")) + " ")
		if v, ok := syntax.ParseEmbedded(l.Line); ok {
			b.WriteString(syntax.Compact(v))
		} else {
			b.WriteString(colored(theme.Text, l.Line))
		}
		b.WriteString("\n")
	}
}
