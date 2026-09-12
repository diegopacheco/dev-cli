package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/backend"
	"github.com/diegopacheco/dev-cli/internal/sys"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

type Tab interface {
	Title() string
	Root() tview.Primitive
	FocusTarget() tview.Primitive
	Show()
	Hide()
	Typing() bool
	Hints() string
}

type App struct {
	app        *tview.Application
	root       *tview.Pages
	pages      *tview.Pages
	bar        *tabBar
	status     *tview.TextView
	tabs       []Tab
	current    int
	queue      func(func())
	Dashboard  *Dashboard
	Processes  *Processes
	Containers *Containers
	Threads    *Threads
	Consoles   []*Console
}

type tabBar struct {
	*tview.Box
	app    *App
	ranges [][2]int
}

func (b *tabBar) Draw(screen tcell.Screen) {
	b.DrawForSubclass(screen, b)
	x, y, w, _ := b.GetRect()
	fill(screen, x, y, w, 1, tcell.StyleDefault.Background(theme.Color(theme.Panel)))
	logo := " ⚡ devcli "
	cx := x
	n := len([]rune(logo))
	for i, r := range logo {
		c := theme.Lerp(theme.Cyan, theme.Magenta, float64(i)/float64(max(1, n-1)))
		screen.SetContent(cx, y, r, nil, tcell.StyleDefault.Background(theme.Color(theme.Bg)).Foreground(c).Bold(true))
		cx += max(1, runewidth.RuneWidth(r))
	}
	cx++
	clock := time.Now().Format(" 15:04:05 ")
	limit := x + w - len(clock)
	b.ranges = b.ranges[:0]
	for i, t := range b.app.tabs {
		text := fmt.Sprintf(" %d %s ", i+1, t.Title())
		st := tcell.StyleDefault.Background(theme.Color(theme.Panel)).Foreground(theme.Color(theme.Dim))
		numSt := st.Foreground(theme.Color(theme.Border))
		if i == b.app.current {
			st = tcell.StyleDefault.Background(theme.Color(theme.Magenta)).Foreground(theme.Color(theme.Bg)).Bold(true)
			numSt = st
		}
		start := cx
		num := fmt.Sprintf(" %d", i+1)
		cx = put(screen, cx, y, limit, num, numSt)
		cx = put(screen, cx, y, limit, text[len(num):], st)
		b.ranges = append(b.ranges, [2]int{start, cx})
	}
	put(screen, limit, y, x+w, clock, tcell.StyleDefault.Background(theme.Color(theme.Panel)).Foreground(theme.Color(theme.Cyan)))
}

func (b *tabBar) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return b.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		x, y := event.Position()
		if action != tview.MouseLeftClick || !b.InRect(x, y) {
			return false, nil
		}
		for i, r := range b.ranges {
			if x >= r[0] && x < r[1] {
				b.app.Switch(i)
				return true, nil
			}
		}
		return false, nil
	})
}

type Targets struct {
	MySQL, Postgres, SQLite, Cassandra, Redis, Loki, Grafana, GrafanaToken string
}

func applyStyles() {
	tview.Styles.PrimitiveBackgroundColor = theme.Color(theme.Bg)
	tview.Styles.ContrastBackgroundColor = theme.Color("#1b2340")
	tview.Styles.MoreContrastBackgroundColor = theme.Color(theme.Magenta)
	tview.Styles.BorderColor = theme.Color(theme.Border)
	tview.Styles.TitleColor = theme.Color(theme.Cyan)
	tview.Styles.GraphicsColor = theme.Color(theme.Border)
	tview.Styles.PrimaryTextColor = theme.Color(theme.Text)
	tview.Styles.SecondaryTextColor = theme.Color(theme.Magenta)
	tview.Styles.TertiaryTextColor = theme.Color(theme.Lime)
	tview.Styles.InverseTextColor = theme.Color(theme.Bg)
	tview.Styles.ContrastSecondaryTextColor = theme.Color(theme.Cyan)
	tview.Borders.Horizontal = '─'
	tview.Borders.Vertical = '│'
	tview.Borders.TopLeft = '╭'
	tview.Borders.TopRight = '╮'
	tview.Borders.BottomLeft = '╰'
	tview.Borders.BottomRight = '╯'
	tview.Borders.HorizontalFocus = '━'
	tview.Borders.VerticalFocus = '┃'
	tview.Borders.TopLeftFocus = '┏'
	tview.Borders.TopRightFocus = '┓'
	tview.Borders.BottomLeftFocus = '┗'
	tview.Borders.BottomRightFocus = '┛'
}

func New(targets Targets, queue func(func())) *App {
	applyStyles()
	a := &App{app: tview.NewApplication(), queue: queue}
	if a.queue == nil {
		a.queue = func(f func()) { a.app.QueueUpdateDraw(f) }
	}
	run := sys.Run
	focus := func(p tview.Primitive) { a.app.SetFocus(p) }
	a.Dashboard = NewDashboard(run, a.queue)
	a.Processes = NewProcesses(run, a.queue, focus, a.confirm)
	a.Containers = NewContainers(run, a.queue, a.confirm, a.app.Suspend)
	a.Threads = NewThreads(run, a.queue, focus)
	a.tabs = []Tab{a.Dashboard, a.Processes, a.Containers, a.Threads}
	backends := []backend.Backend{
		backend.NewMySQL(targets.MySQL),
		backend.NewPostgres(targets.Postgres),
		backend.NewSQLite(targets.SQLite),
		backend.NewCassandra(targets.Cassandra),
		backend.NewRedis(targets.Redis),
		backend.NewLoki(targets.Loki),
		backend.NewGrafana(targets.Grafana, targets.GrafanaToken),
	}
	for _, b := range backends {
		c := NewConsole(b.Name(), b, a.queue, focus)
		a.Consoles = append(a.Consoles, c)
		a.tabs = append(a.tabs, c)
	}
	a.pages = tview.NewPages()
	for i, t := range a.tabs {
		a.pages.AddPage(t.Title(), t.Root(), true, i == 0)
	}
	a.bar = &tabBar{Box: tview.NewBox(), app: a}
	a.status = newInfo()
	a.status.SetBackgroundColor(theme.Color(theme.Panel))
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.bar, 1, 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.status, 1, 0, false)
	a.root = tview.NewPages().AddPage("main", layout, true, true)
	a.app.SetRoot(a.root, true).EnableMouse(true).EnablePaste(true)
	a.app.SetInputCapture(a.keys)
	return a
}

func (a *App) TabIndex(name string) int {
	for i, t := range a.tabs {
		if strings.EqualFold(t.Title(), name) {
			return i
		}
	}
	return -1
}

func (a *App) Switch(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	if i != a.current {
		a.tabs[a.current].Hide()
	}
	a.current = i
	t := a.tabs[i]
	a.pages.SwitchToPage(t.Title())
	t.Show()
	a.app.SetFocus(t.FocusTarget())
	a.status.SetText(" " + colored(theme.Cyan, t.Title()) + colored(theme.Dim, " │ ") + colored(theme.Text, t.Hints()) + colored(theme.Dim, " │ Ctrl-N/P tabs · F1-F11 · Ctrl-Q quit"))
}

func (a *App) keys(event *tcell.EventKey) *tcell.EventKey {
	if name, _ := a.root.GetFrontPage(); name != "main" {
		return event
	}
	switch key := event.Key(); {
	case key == tcell.KeyCtrlQ || key == tcell.KeyCtrlC:
		a.app.Stop()
		return nil
	case key == tcell.KeyCtrlN:
		a.Switch((a.current + 1) % len(a.tabs))
		return nil
	case key == tcell.KeyCtrlP:
		a.Switch((a.current - 1 + len(a.tabs)) % len(a.tabs))
		return nil
	case key >= tcell.KeyF1 && key <= tcell.KeyF11:
		a.Switch(int(key - tcell.KeyF1))
		return nil
	case key == tcell.KeyRune && !a.tabs[a.current].Typing():
		r := event.Rune()
		if r == 'q' {
			a.app.Stop()
			return nil
		}
		if r >= '1' && r <= '9' {
			a.Switch(int(r - '1'))
			return nil
		}
	}
	return event
}

func (a *App) confirm(title, text, action string, onYes func()) {
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{"Cancel", action}).
		SetTextColor(theme.Color(theme.Text)).
		SetBackgroundColor(theme.Color("#2a0f1a")).
		SetButtonBackgroundColor(theme.Color(theme.Border)).
		SetButtonTextColor(theme.Color(theme.Text)).
		SetButtonActivatedStyle(tcell.StyleDefault.Background(theme.Color(theme.Red)).Foreground(theme.Color(theme.Bg)).Bold(true))
	modal.SetBorderColor(theme.Color(theme.Red)).SetTitle(" ⚠ " + title + " ").SetTitleColor(theme.Color(theme.Red))
	prevFocus := a.app.GetFocus()
	closeModal := func(yes bool) {
		a.root.RemovePage("confirm")
		a.app.SetFocus(prevFocus)
		if yes {
			onYes()
		}
	}
	modal.SetDoneFunc(func(index int, label string) { closeModal(label == action) })
	modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'y':
			closeModal(true)
			return nil
		case 'n':
			closeModal(false)
			return nil
		}
		if event.Key() == tcell.KeyEscape {
			closeModal(false)
			return nil
		}
		return event
	})
	modal.SetFocus(0)
	a.root.AddPage("confirm", modal, true, true)
	a.app.SetFocus(modal)
}

func (a *App) Run(tab string) error {
	start := 0
	if i := a.TabIndex(tab); i >= 0 {
		start = i
	}
	a.Dashboard.Show()
	a.Switch(start)
	go func() {
		for range time.Tick(time.Second) {
			a.app.QueueUpdateDraw(func() {})
		}
	}()
	return a.app.Run()
}
