package ui

import (
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func newTable(title, color string) *tview.Table {
	t := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	panelBox(t.Box, title, color)
	t.SetSelectedStyle(tcell.StyleDefault.Background(theme.Color("#2b1a4a")).Foreground(theme.Color(theme.Magenta)).Bold(true))
	return t
}

func headerRow(t *tview.Table, names ...string) {
	for i, n := range names {
		t.SetCell(0, i, tview.NewTableCell(n).SetTextColor(theme.Color(theme.Cyan)).SetSelectable(false).SetAttributes(tcell.AttrBold).SetBackgroundColor(theme.Color(theme.Panel)))
	}
}

func cell(text, color string) *tview.TableCell {
	return tview.NewTableCell(tview.Escape(text)).SetTextColor(theme.Color(color))
}

func newFilter(label string) *tview.InputField {
	f := tview.NewInputField().
		SetLabel(label).
		SetLabelColor(theme.Color(theme.Magenta)).
		SetFieldBackgroundColor(theme.Color("#1b2340")).
		SetFieldTextColor(theme.Color(theme.Text)).
		SetPlaceholder("type to filter, Enter to apply").
		SetPlaceholderTextColor(theme.Color(theme.Dim))
	f.SetBackgroundColor(theme.Color(theme.Bg))
	return f
}

func newInfo() *tview.TextView {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBackgroundColor(theme.Color(theme.Bg))
	return v
}

type confirmFunc func(title, text, action string, onYes func())
