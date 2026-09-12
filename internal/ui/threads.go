package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/diegopacheco/dev-cli/internal/jvm"
	"github.com/diegopacheco/dev-cli/internal/sys"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var stateFilters = []string{"", "RUNNABLE", "WAITING", "TIMED_WAITING", "BLOCKED"}

type Threads struct {
	*tview.Flex
	run         sys.Runner
	queue       func(func())
	focus       func(tview.Primitive)
	table       *tview.Table
	filter      *tview.InputField
	dumpView    *tview.TextView
	info        *tview.TextView
	jvms        []jvm.Process
	dump        jvm.Dump
	dumpPID     int
	dumpLang    jvm.Language
	stateFilter int
	visible     atomic.Bool
	loop        bool
}

func NewThreads(run sys.Runner, queue func(func()), focus func(tview.Primitive)) *Threads {
	t := &Threads{Flex: tview.NewFlex().SetDirection(tview.FlexRow), run: run, queue: queue, focus: focus}
	t.table = newTable(" ☕ jvms ", theme.Orange)
	t.table.SetInputCapture(t.keys)
	t.table.SetSelectedFunc(func(row, col int) { t.DumpSelected() })
	t.filter = newFilter(" ⌕ threads ▸ ")
	t.filter.SetChangedFunc(func(string) { t.render() })
	t.filter.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			t.filter.SetText("")
		}
		t.focus(t.table)
	})
	t.dumpView = tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	panelBox(t.dumpView.Box, " ≣ thread dump ", theme.Lime)
	t.dumpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyEscape {
			t.focus(t.table)
			return nil
		}
		return t.keys(event)
	})
	t.info = newInfo()
	right := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(t.filter, 1, 0, false).AddItem(t.dumpView, 0, 1, false)
	body := tview.NewFlex().AddItem(t.table, 44, 0, true).AddItem(right, 0, 1, false)
	t.AddItem(body, 0, 1, true).AddItem(t.info, 1, 0, false)
	t.SetBackgroundColor(theme.Color(theme.Bg))
	return t
}

func (t *Threads) Title() string                { return "Threads" }
func (t *Threads) Root() tview.Primitive        { return t }
func (t *Threads) FocusTarget() tview.Primitive { return t.table }
func (t *Threads) Typing() bool                 { return t.filter.HasFocus() }
func (t *Threads) Hide()                        { t.visible.Store(false) }
func (t *Threads) Hints() string {
	return "Enter dump · Tab focus dump · / filter threads · s state filter · r refresh jvms"
}

func (t *Threads) Show() {
	t.visible.Store(true)
	if t.loop {
		return
	}
	t.loop = true
	go func() {
		for {
			if t.visible.Load() {
				t.Discover()
			}
			time.Sleep(5 * time.Second)
		}
	}()
}

func (t *Threads) setInfo(color, text string) {
	t.info.SetText(" " + colored(color, text))
}

func (t *Threads) Discover() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := jvm.Discover(ctx, t.run)
	t.queue(func() {
		if err != nil {
			t.setInfo(theme.Red, "✖ "+err.Error())
			return
		}
		t.ApplyJVMs(list)
	})
}

func langColor(l jvm.Language) string {
	switch l {
	case jvm.Scala:
		return theme.Red
	case jvm.Kotlin:
		return theme.Purple
	case jvm.Clojure:
		return theme.Lime
	}
	return theme.Orange
}

func (t *Threads) ApplyJVMs(list []jvm.Process) {
	row, _ := t.table.GetSelection()
	t.jvms = list
	t.table.Clear()
	headerRow(t.table, "PID", "LANG", "MAIN")
	for i, p := range list {
		t.table.SetCell(i+1, 0, cell(fmt.Sprint(p.PID), theme.Dim))
		t.table.SetCell(i+1, 1, cell("◆ "+string(p.Language), langColor(p.Language)))
		t.table.SetCell(i+1, 2, cell(p.Main, theme.Text).SetExpansion(1))
	}
	if len(list) > 0 {
		t.table.Select(max(1, min(row, len(list))), 0)
	}
	if t.dumpPID == 0 {
		t.setInfo(theme.Lime, fmt.Sprintf("%d JVMs found · Enter to dump threads", len(list)))
	}
}

func (t *Threads) SelectPID(pid int) bool {
	for i, p := range t.jvms {
		if p.PID == pid {
			t.table.Select(i+1, 0)
			return true
		}
	}
	return false
}

func (t *Threads) DumpSelected() {
	row, _ := t.table.GetSelection()
	if row < 1 || row > len(t.jvms) {
		return
	}
	p := t.jvms[row-1]
	t.setInfo(theme.Yellow, fmt.Sprintf("◌ jcmd %d Thread.print…", p.PID))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		d, err := jvm.ThreadDump(ctx, t.run, p.PID)
		t.queue(func() {
			if err != nil {
				t.setInfo(theme.Red, "✖ "+err.Error())
				return
			}
			t.ApplyDump(p, d)
		})
	}()
}

func (t *Threads) ApplyDump(p jvm.Process, d jvm.Dump) {
	t.dump, t.dumpPID, t.dumpLang = d, p.PID, p.Language
	t.render()
	t.dumpView.ScrollToBeginning()
	msg := fmt.Sprintf("✔ %d threads from pid %d (%s)", len(d.Threads), p.PID, p.Language)
	if len(d.Deadlocks) > 0 {
		t.setInfo(theme.Red, msg+" · DEADLOCK DETECTED")
		return
	}
	t.setInfo(theme.Lime, msg)
}

func threadStateColor(state string) string {
	switch state {
	case "RUNNABLE":
		return theme.Lime
	case "WAITING":
		return theme.Yellow
	case "TIMED_WAITING":
		return theme.Cyan
	case "BLOCKED":
		return theme.Red
	}
	return theme.Dim
}

func (t *Threads) matches(th jvm.Thread) bool {
	if sf := stateFilters[t.stateFilter]; sf != "" && th.State != sf {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(t.filter.GetText()))
	if q == "" || strings.Contains(strings.ToLower(th.Name), q) {
		return true
	}
	for _, f := range th.Frames {
		if strings.Contains(strings.ToLower(jvm.Demangle(t.dumpLang, f.Text)), q) {
			return true
		}
	}
	return false
}

func (t *Threads) render() {
	if t.dumpPID == 0 {
		t.dumpView.SetText(colored(theme.Dim, "\n  select a JVM and press Enter to take a thread dump"))
		return
	}
	t.dumpView.SetText(RenderDump(t.dumpLang, t.dumpPID, t.dump, stateFilters[t.stateFilter], t.matches))
}

func RenderDump(lang jvm.Language, pid int, d jvm.Dump, state string, match func(jvm.Thread) bool) string {
	var b strings.Builder
	b.WriteString(colored(langColor(lang), fmt.Sprintf(" ◆ %s ", lang)) + colored(theme.Dim, fmt.Sprintf(" pid %d · ", pid)) + colored(theme.Text, d.Header) + "\n ")
	counts := d.Counts()
	states := make([]string, 0, len(counts))
	for s := range counts {
		states = append(states, s)
	}
	sort.Strings(states)
	b.WriteString(colored(theme.Text, fmt.Sprintf("%d threads", len(d.Threads))))
	for _, s := range states {
		b.WriteString("  " + colored(threadStateColor(s), fmt.Sprintf("● %s %d", s, counts[s])))
	}
	if sf := state; sf != "" {
		b.WriteString(colored(theme.Magenta, "   filter: "+sf))
	}
	b.WriteString("\n")
	if len(d.Deadlocks) > 0 {
		b.WriteString("\n" + "[" + theme.Bg + ":" + theme.Red + ":b] ☠ DEADLOCK [-:-:-]\n")
		for _, l := range d.Deadlocks {
			if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "===") {
				continue
			}
			b.WriteString(colored(theme.Red, "  "+l) + "\n")
		}
	}
	shown := 0
	for _, th := range d.Threads {
		if !match(th) {
			continue
		}
		shown++
		b.WriteString("\n")
		nameColor := theme.Magenta
		prefix := ""
		if th.Deadlocked {
			nameColor, prefix = theme.Red, "☠ "
		}
		b.WriteString("[" + nameColor + "::b]" + tview.Escape(prefix+`"`+th.Name+`"`) + "[-::-] ")
		b.WriteString(colored(threadStateColor(th.State), th.State) + " " + colored(theme.Dim, th.Detail))
		if th.Daemon {
			b.WriteString(colored(theme.Dim, " daemon"))
		}
		if th.CPU != "" {
			b.WriteString(colored(theme.Dim, " cpu=") + colored(theme.Orange, th.CPU))
		}
		b.WriteString("\n")
		for _, f := range th.Frames {
			if f.Lock {
				b.WriteString(colored(theme.Orange, "      ⊸ "+f.Text) + "\n")
				continue
			}
			color := theme.Text
			if jvm.IsRuntimeFrame(f.Text) {
				color = theme.Dim
			}
			b.WriteString(colored(theme.Border, "    at ") + colored(color, jvm.Demangle(lang, f.Text)) + "\n")
		}
	}
	if shown == 0 {
		b.WriteString(colored(theme.Dim, "\n  no threads match the filter\n"))
	}
	return b.String()
}

func (t *Threads) keys(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Rune() == '/':
		t.focus(t.filter)
	case event.Rune() == 's':
		t.stateFilter = (t.stateFilter + 1) % len(stateFilters)
		t.render()
	case event.Rune() == 'r':
		go t.Discover()
	case event.Key() == tcell.KeyTab:
		t.focus(t.dumpView)
	default:
		return event
	}
	return nil
}
