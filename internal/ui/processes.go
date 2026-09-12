package ui

import (
	"context"
	"fmt"
	"syscall"
	"time"

	"github.com/diegopacheco/dev-cli/internal/sys"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Processes struct {
	*tview.Flex
	run     sys.Runner
	queue   func(func())
	focus   func(tview.Primitive)
	confirm confirmFunc
	filter  *tview.InputField
	table   *tview.Table
	detail  *tview.TextView
	info    *tview.TextView
	all     []sys.Process
	shown   []sys.Process
	sortKey sys.SortKey
	visible bool
	loop    bool
}

func NewProcesses(run sys.Runner, queue func(func()), focus func(tview.Primitive), confirm confirmFunc) *Processes {
	p := &Processes{Flex: tview.NewFlex().SetDirection(tview.FlexRow), run: run, queue: queue, focus: focus, confirm: confirm}
	p.filter = newFilter(" ⌕ filter ▸ ")
	p.filter.SetChangedFunc(func(string) { p.render() })
	p.filter.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			p.filter.SetText("")
		}
		p.focus(p.table)
	})
	p.table = newTable(" ☰ processes ", theme.Purple)
	p.table.SetSelectionChangedFunc(func(row, col int) { p.showDetail() })
	p.table.SetInputCapture(p.keys)
	p.detail = newInfo()
	panelBox(p.detail.Box, " ◈ selected ", theme.Cyan)
	p.info = newInfo()
	p.AddItem(p.filter, 1, 0, false).AddItem(p.table, 0, 1, true).AddItem(p.detail, 4, 0, false).AddItem(p.info, 1, 0, false)
	p.SetBackgroundColor(theme.Color(theme.Bg))
	return p
}

func (p *Processes) Title() string                { return "Processes" }
func (p *Processes) Root() tview.Primitive        { return p }
func (p *Processes) FocusTarget() tview.Primitive { return p.table }
func (p *Processes) Typing() bool                 { return p.filter.HasFocus() }
func (p *Processes) Hide()                        { p.visible = false }
func (p *Processes) Hints() string {
	return "/ filter · o sort CPU/MEM/PID · x SIGTERM · K SIGKILL · r refresh · Esc clear filter"
}

func (p *Processes) Show() {
	p.visible = true
	if p.loop {
		return
	}
	p.loop = true
	go func() {
		for {
			if p.visible {
				p.Refresh()
			}
			time.Sleep(2 * time.Second)
		}
	}()
}

func (p *Processes) Refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	procs, err := sys.Processes(ctx, p.run)
	p.queue(func() {
		if err != nil {
			p.setInfo(theme.Red, "✖ "+err.Error())
			return
		}
		p.all = procs
		p.render()
	})
}

func (p *Processes) setInfo(color, text string) {
	p.info.SetText(" " + colored(color, text))
}

func (p *Processes) SetFilter(text string) {
	p.filter.SetText(text)
	p.render()
}

func (p *Processes) selected() (sys.Process, bool) {
	row, _ := p.table.GetSelection()
	if row < 1 || row > len(p.shown) {
		return sys.Process{}, false
	}
	return p.shown[row-1], true
}

func (p *Processes) render() {
	prev, hadPrev := p.selected()
	list := append([]sys.Process{}, p.all...)
	sys.SortProcesses(list, p.sortKey)
	p.shown = sys.FilterProcesses(list, p.filter.GetText())
	p.table.Clear()
	cols := []string{"PID", "PPID", "USER", "CPU%", "MEM%", "RSS", "TIME", "NAME", "COMMAND"}
	for i, c := range cols {
		if (c == "CPU%" && p.sortKey == sys.ByCPU) || (c == "RSS" && p.sortKey == sys.ByMem) || (c == "PID" && p.sortKey == sys.ByPID) {
			cols[i] = c + " ▼"
		}
	}
	headerRow(p.table, cols...)
	selectRow := 1
	for i, proc := range p.shown {
		r := i + 1
		p.table.SetCell(r, 0, cell(fmt.Sprint(proc.PID), theme.Dim).SetAlign(tview.AlignRight))
		p.table.SetCell(r, 1, cell(fmt.Sprint(proc.PPID), theme.Dim).SetAlign(tview.AlignRight))
		p.table.SetCell(r, 2, cell(proc.User, theme.Purple))
		p.table.SetCell(r, 3, cell(fmt.Sprintf("%.1f", proc.CPU), theme.Hex(theme.Heat(proc.CPU/100))).SetAlign(tview.AlignRight))
		p.table.SetCell(r, 4, cell(fmt.Sprintf("%.1f", proc.Mem), theme.Hex(theme.Heat(proc.Mem/25))).SetAlign(tview.AlignRight))
		p.table.SetCell(r, 5, cell(bytesLabel(float64(proc.RSS)), theme.Magenta).SetAlign(tview.AlignRight))
		p.table.SetCell(r, 6, cell(proc.Elapsed, theme.Dim).SetAlign(tview.AlignRight))
		p.table.SetCell(r, 7, cell(proc.Name(), theme.Lime))
		p.table.SetCell(r, 8, cell(proc.Command, theme.Text).SetExpansion(1))
		if hadPrev && proc.PID == prev.PID {
			selectRow = r
		}
	}
	if len(p.shown) > 0 {
		p.table.Select(selectRow, 0)
	}
	p.setInfo(theme.Lime, fmt.Sprintf("%d of %d processes · sorted by %s", len(p.shown), len(p.all), p.sortKey))
	p.showDetail()
}

func (p *Processes) showDetail() {
	proc, ok := p.selected()
	if !ok {
		p.detail.SetText("")
		return
	}
	p.detail.SetText(fmt.Sprintf("%s %s  %s %d  %s %s  %s %.1f%%  %s %s\n%s",
		colored(theme.Dim, "pid"), colored(theme.Magenta, fmt.Sprint(proc.PID)),
		colored(theme.Dim, "parent"), proc.PPID,
		colored(theme.Dim, "user"), colored(theme.Purple, proc.User),
		colored(theme.Dim, "cpu"), proc.CPU,
		colored(theme.Dim, "rss"), colored(theme.Magenta, bytesLabel(float64(proc.RSS))),
		colored(theme.Lime, proc.Command)))
}

func (p *Processes) keys(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyEscape:
		p.SetFilter("")
	case event.Rune() == '/':
		p.focus(p.filter)
	case event.Rune() == 'o':
		p.sortKey = (p.sortKey + 1) % 3
		p.render()
	case event.Rune() == 'r':
		go p.Refresh()
	case event.Rune() == 'x':
		p.signal(syscall.SIGTERM, "SIGTERM")
	case event.Rune() == 'K':
		p.signal(syscall.SIGKILL, "SIGKILL")
	default:
		return event
	}
	return nil
}

func (p *Processes) signal(sig syscall.Signal, name string) {
	proc, ok := p.selected()
	if !ok {
		return
	}
	text := fmt.Sprintf("Send %s to pid %d?\n\n%s", name, proc.PID, proc.Command)
	p.confirm("kill process", text, name, func() {
		err := sys.Signal(context.Background(), p.run, proc, sig)
		if err != nil {
			p.setInfo(theme.Red, "✖ "+err.Error())
			return
		}
		p.setInfo(theme.Lime, fmt.Sprintf("✔ %s sent to %d (%s)", name, proc.PID, proc.Name()))
		go p.Refresh()
	})
}
