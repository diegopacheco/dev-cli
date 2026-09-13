package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/diegopacheco/dev-cli/internal/backend"
	"github.com/diegopacheco/dev-cli/internal/discover"
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
	Palette    *Palette
	found      []discover.Found
	scanning   bool
	scanErr    error
	splashOn   atomic.Bool
	NoDiscover bool
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
	MySQL, Postgres, SQLite, Cassandra, Redis, Loki, Grafana, GrafanaToken, Prometheus string
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
		backend.NewPrometheus(targets.Prometheus),
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
	a.hints("")
}

func (a *App) hints(flash string) {
	t := a.tabs[a.current]
	text := " " + colored(theme.Cyan, t.Title()) + colored(theme.Dim, " │ ")
	if flash != "" {
		text += flash + colored(theme.Dim, " │ ")
	}
	a.status.SetText(text + colored(theme.Text, t.Hints()) + colored(theme.Dim, " │ ⌘K search · ⌘/ keys · Ctrl-Q quit"))
}

func isMeta(event *tcell.EventKey, r rune) bool {
	return event.Key() == tcell.KeyRune && event.Rune() == r && event.Modifiers()&(tcell.ModMeta|tcell.ModCtrl) != 0
}

func (a *App) keys(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyCtrlQ || event.Key() == tcell.KeyCtrlC {
		a.app.Stop()
		return nil
	}
	if name, _ := a.root.GetFrontPage(); name != "main" {
		return event
	}
	if event.Key() == tcell.KeyCtrlK || isMeta(event, 'k') {
		a.OpenPalette()
		return nil
	}
	if event.Key() == tcell.KeyCtrlUnderscore || isMeta(event, '/') {
		a.OpenShortcuts()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Modifiers()&tcell.ModMeta != 0 && event.Rune() >= '0' && event.Rune() <= '9' {
		n := int(event.Rune() - '0')
		if n == 0 {
			n = 10
		}
		a.Switch(n - 1)
		return nil
	}
	switch key := event.Key(); {
	case key == tcell.KeyCtrlN:
		a.Switch((a.current + 1) % len(a.tabs))
		return nil
	case key == tcell.KeyCtrlP:
		a.Switch((a.current - 1 + len(a.tabs)) % len(a.tabs))
		return nil
	case key >= tcell.KeyF1 && key <= tcell.KeyF12:
		a.Switch(int(key - tcell.KeyF1))
		return nil
	case key == tcell.KeyRune && !a.tabs[a.current].Typing():
		r := event.Rune()
		if r == 'q' {
			a.app.Stop()
			return nil
		}
		if r == '?' {
			a.OpenShortcuts()
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
	a.ShowSplash()
	if !a.NoDiscover {
		a.Discover(false)
	}
	go func() {
		for a.splashOn.Load() {
			time.Sleep(80 * time.Millisecond)
			a.app.QueueUpdateDraw(func() {})
		}
	}()
	time.AfterFunc(1800*time.Millisecond, func() { a.queue(a.CloseSplash) })
	go func() {
		for range time.Tick(time.Second) {
			a.app.QueueUpdateDraw(func() {})
		}
	}()
	return a.app.Run()
}

func (a *App) overlay(name string, p tview.Primitive) {
	a.root.RemovePage(name)
	a.root.AddPage(name, p, true, true)
	a.app.SetFocus(p)
}

func (a *App) closeOverlay(name string) {
	a.root.RemovePage(name)
	if front, p := a.root.GetFrontPage(); front != "main" {
		a.app.SetFocus(p)
		return
	}
	a.app.SetFocus(a.tabs[a.current].FocusTarget())
}

func (a *App) OpenPalette() {
	if front, _ := a.root.GetFrontPage(); front == "palette" {
		a.closeOverlay("palette")
		return
	}
	a.Palette = NewPalette(a.PaletteItems, func() { a.closeOverlay("palette") })
	a.overlay("palette", a.Palette)
}

func (a *App) OpenShortcuts() {
	a.overlay("shortcuts", NewShortcuts(func() { a.closeOverlay("shortcuts") }))
}

func (a *App) ShowSplash() {
	a.splashOn.Store(true)
	a.overlay("splash", NewSplash(a.splashStatus, a.CloseSplash))
}

func (a *App) CloseSplash() {
	if !a.splashOn.Swap(false) {
		return
	}
	a.closeOverlay("splash")
	a.promptFound()
}

func (a *App) splashStatus() (string, string) {
	switch {
	case a.NoDiscover:
		return theme.Dim, "container discovery off"
	case a.scanning:
		return theme.Yellow, "scanning podman / docker for databases…"
	case a.scanErr != nil:
		return theme.Red, "container scan failed: " + a.scanErr.Error()
	}
	return theme.Lime, fmt.Sprintf("found %d data containers", len(a.found))
}

func (a *App) Discover(announce bool) {
	a.scanning = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		found, err := discover.Containers(ctx, sys.Run)
		a.queue(func() {
			a.scanning = false
			a.found, a.scanErr = found, err
			switch {
			case err != nil && announce:
				a.hints(colored(theme.Red, "✖ container scan: "+err.Error()))
			case len(found) == 0 && announce:
				a.hints(colored(theme.Yellow, "no database, cache or observability containers running"))
			}
			if !a.splashOn.Load() {
				a.promptFound()
			}
		})
	}()
}

func (a *App) SetFound(found []discover.Found) {
	a.found = found
}

func (a *App) promptFound() {
	if len(a.found) == 0 || a.scanning {
		return
	}
	if front, _ := a.root.GetFrontPage(); front != "main" {
		return
	}
	found := a.found
	a.found = nil
	a.overlay("connect", NewConnectPrompt(found, a.ConnectFound, func() { a.closeOverlay("connect") }))
}

func (a *App) console(kind string) (*Console, int) {
	for i, t := range a.tabs {
		if c, ok := t.(*Console); ok && strings.EqualFold(c.Title(), kind) {
			return c, i
		}
	}
	return nil, -1
}

func (a *App) ConnectFound(chosen []discover.Found) {
	var names []string
	for _, f := range chosen {
		if c, _ := a.console(f.Kind); c != nil {
			c.SetTarget(f.Target)
			names = append(names, f.Kind)
		}
	}
	if len(names) > 0 {
		a.hints(colored(theme.Lime, "✔ connecting "+strings.Join(names, ", ")))
	}
}

func (a *App) PaletteItems(query string) []PaletteItem {
	var items []PaletteItem
	for i, t := range a.tabs {
		i := i
		keys := fmt.Sprintf("F%d", i+1)
		if i < 10 {
			keys = fmt.Sprintf("⌘%d · F%d", (i+1)%10, i+1)
		}
		items = append(items, PaletteItem{Kind: "tab", Title: t.Title(), Detail: keys, Run: func() { a.Switch(i) }})
	}
	current, _ := a.tabs[a.current].(*Console)
	actions := []PaletteItem{
		{Kind: "action", Title: "Show all shortcuts", Detail: "⌘/", Run: a.OpenShortcuts},
		{Kind: "action", Title: "Discover containers and connect", Detail: "podman / docker", Run: func() { a.Discover(true) }},
		{Kind: "action", Title: "Refresh processes", Detail: "Processes", Run: func() { a.Switch(1); go a.Processes.Refresh() }},
		{Kind: "action", Title: "Refresh containers", Detail: "Containers", Run: func() { a.Switch(2); go a.Containers.Refresh() }},
		{Kind: "action", Title: "Refresh JVMs", Detail: "Threads", Run: func() { a.Switch(3); go a.Threads.Discover() }},
		{Kind: "action", Title: "Quit devcli", Detail: "Ctrl-Q", Run: a.app.Stop},
	}
	if current != nil {
		c := current
		actions = append(actions,
			PaletteItem{Kind: "action", Title: "Run editor buffer", Detail: c.Title() + " · Ctrl-R", Run: func() { c.editor.run() }},
			PaletteItem{Kind: "action", Title: "Toggle table / JSON", Detail: c.Title() + " · Ctrl-T", Run: c.ToggleJSON},
			PaletteItem{Kind: "action", Title: "Reload completion words", Detail: c.Title() + " · F5", Run: c.ReloadWords},
			PaletteItem{Kind: "action", Title: "Clear editor", Detail: c.Title() + " · Ctrl-L", Run: func() { c.editor.SetText("") }},
			PaletteItem{Kind: "action", Title: "Reconnect", Detail: MaskTarget(c.Target()), Run: c.Connect},
		)
		if c.HasCatalog() {
			actions = append(actions, PaletteItem{Kind: "action", Title: "List everything available (all)", Detail: c.Title() + " · all", Run: c.RunAll})
		}
	}
	items = append(items, actions...)
	readyItems := func(i int, c *Console) {
		for _, r := range c.Ready() {
			r := r
			items = append(items, PaletteItem{Kind: "ready", Title: r[0], Detail: c.Title() + " · " + r[1], Run: func() {
				a.Switch(i)
				c.editor.SetText(r[0])
			}})
		}
	}
	if current != nil {
		readyItems(a.current, current)
	}
	if strings.TrimSpace(query) == "" {
		return items
	}
	for i, t := range a.tabs {
		if c, ok := t.(*Console); ok && c != current {
			readyItems(i, c)
		}
	}
	for _, f := range a.found {
		f := f
		items = append(items, PaletteItem{Kind: "connect", Title: "Connect " + f.Kind + " to " + f.Container, Detail: MaskTarget(f.Target), Run: func() {
			a.ConnectFound([]discover.Found{f})
			_, i := a.console(f.Kind)
			a.Switch(i)
		}})
	}
	for _, ct := range a.Containers.list {
		ct := ct
		items = append(items, PaletteItem{Kind: "container", Title: ct.Name, Detail: ct.State + " · " + ct.Image, Run: func() {
			a.Switch(2)
			a.Containers.SelectID(ct.ID)
		}})
	}
	for _, p := range a.Threads.jvms {
		p := p
		items = append(items, PaletteItem{Kind: "jvm", Title: p.Main, Detail: fmt.Sprintf("%s · pid %d · thread dump", p.Language, p.PID), Run: func() {
			a.Switch(3)
			a.Threads.SelectPID(p.PID)
			a.Threads.DumpSelected()
		}})
	}
	procs := a.Processes.all
	if len(procs) == 0 {
		procs = a.Dashboard.procs
	}
	for _, p := range procs {
		p := p
		items = append(items, PaletteItem{Kind: "process", Title: p.Name(), Detail: fmt.Sprintf("pid %d · %s · %.1f%% cpu", p.PID, p.User, p.CPU), Run: func() {
			a.Switch(1)
			a.Processes.SetFilter(strconv.Itoa(p.PID))
		}})
	}
	for i, t := range a.tabs {
		c, ok := t.(*Console)
		if !ok {
			continue
		}
		i := i
		for n, w := range c.editor.Words() {
			if n >= 2000 {
				break
			}
			w := w
			items = append(items, PaletteItem{Kind: strings.ToLower(c.Title()), Title: w, Detail: "insert into " + c.Title() + " editor", Run: func() {
				a.Switch(i)
				c.editor.Insert(w)
			}})
		}
		for _, h := range c.editor.History() {
			h := h
			items = append(items, PaletteItem{Kind: "history", Title: strings.ReplaceAll(h, "\n", " "), Detail: c.Title(), Run: func() {
				a.Switch(i)
				c.editor.SetText(h)
			}})
		}
	}
	return items
}
