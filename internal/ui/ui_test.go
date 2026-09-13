package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/diegopacheco/dev-cli/internal/backend"
	"github.com/diegopacheco/dev-cli/internal/discover"
	"github.com/diegopacheco/dev-cli/internal/syntax"
	"github.com/diegopacheco/dev-cli/internal/sys"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

func typeText(e *Editor, text string) {
	for _, r := range text {
		if r == '\n' {
			e.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
			continue
		}
		e.HandleKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}

func key(e *Editor, k tcell.Key) {
	e.HandleKey(tcell.NewEventKey(k, 0, tcell.ModNone))
}

func sqlEditor(ran *[]string) *Editor {
	e := NewEditor(syntax.SQL)
	e.runsOnEnter = backend.EndsWithSemicolon
	e.onRun = func(text string) { *ran = append(*ran, text) }
	e.SetWords([]string{"users", "user_id"})
	return e
}

func TestTabAcceptsCompletionInTheCaseTheUserTyped(t *testing.T) {
	var ran []string
	e := sqlEditor(&ran)
	typeText(e, "sel")
	if len(e.Popup()) == 0 || e.Popup()[0] != "SELECT" {
		t.Fatalf("typing must open the popup as you type, got %v", e.Popup())
	}
	key(e, tcell.KeyTab)
	typeText(e, " * from use")
	key(e, tcell.KeyDown)
	key(e, tcell.KeyTab)
	if e.Text() != "select * from user_id" {
		t.Fatalf("lowercase typing must produce a lowercase keyword and Down must pick the second table, got %q", e.Text())
	}
}

func TestEnterAddsNewlineUntilStatementIsTerminated(t *testing.T) {
	var ran []string
	e := sqlEditor(&ran)
	typeText(e, "select 1\nfrom dual")
	if len(ran) != 0 || e.Text() != "select 1\nfrom dual" {
		t.Fatalf("Enter must not run an unfinished statement, ran=%v text=%q", ran, e.Text())
	}
	typeText(e, ";\n")
	if len(ran) != 1 || ran[0] != "select 1\nfrom dual;" {
		t.Fatalf("Enter after ; must run the whole buffer, got %v", ran)
	}
}

func TestCtrlRRunsWithoutSemicolonAndHistoryRecallsIt(t *testing.T) {
	var ran []string
	e := sqlEditor(&ran)
	typeText(e, "select 42")
	key(e, tcell.KeyCtrlR)
	key(e, tcell.KeyCtrlL)
	typeText(e, "draft")
	key(e, tcell.KeyUp)
	if len(ran) != 1 || e.Text() != "select 42" {
		t.Fatalf("Up on the first line must recall history, got %q", e.Text())
	}
	key(e, tcell.KeyDown)
	if e.Text() != "draft" {
		t.Fatalf("Down past the newest entry must restore the draft, got %q", e.Text())
	}
}

func TestBackspaceJoinsLinesAndEnterKeepsIndent(t *testing.T) {
	var ran []string
	e := sqlEditor(&ran)
	typeText(e, "  a\nb")
	if e.Text() != "  a\n  b" {
		t.Fatalf("new lines keep the indentation of the previous line, got %q", e.Text())
	}
	key(e, tcell.KeyHome)
	key(e, tcell.KeyBackspace2)
	if e.Text() != "  a  b" {
		t.Fatalf("got %q", e.Text())
	}
}

func TestPasteInsertsMultipleLinesWithoutRunning(t *testing.T) {
	var ran []string
	e := sqlEditor(&ran)
	e.PasteHandler()("select 1;\nselect 2;", nil)
	if len(ran) != 0 || e.Text() != "select 1;\nselect 2;" {
		t.Fatalf("pasting a script must not execute it line by line, ran=%v", ran)
	}
}

func TestEditorDrawsLineNumbersAndHighlightsKeywords(t *testing.T) {
	var ran []string
	e := sqlEditor(&ran)
	e.SetText("select 'x'\nfrom t")
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(40, 5)
	e.SetRect(0, 0, 40, 5)
	e.Draw(screen)
	if r, _, _, _ := screen.GetContent(0, 1); r != '2' {
		t.Fatalf("gutter must number lines, got %q", r)
	}
	gutter := e.gutter()
	_, _, st, _ := screen.GetContent(gutter, 0)
	fg, _, _ := st.Decompose()
	if fg != theme.Color(theme.Cyan) {
		t.Fatalf("keyword must be cyan, got %v", fg)
	}
	_, _, st, _ = screen.GetContent(gutter+7, 0)
	if fg, _, _ := st.Decompose(); fg != theme.Color(theme.Lime) {
		t.Fatalf("string must be lime, got %v", fg)
	}
}

func TestResultsTableAndJSONViews(t *testing.T) {
	r := []backend.Result{{Title: "q", Columns: []string{"id", "profile"}, Rows: [][]any{{int64(1), `{"lang":"go"}`}, {int64(2), nil}}}}
	table := RenderResults(r, nil, false)
	if !strings.Contains(table, "┌") || !strings.Contains(table, "NULL") {
		t.Fatalf("table view needs borders and visible NULLs: %s", table)
	}
	js := RenderResults(r, errors.New("boom"), true)
	if !strings.Contains(js, `"lang"`) || strings.Contains(js, `\"lang\"`) || !strings.Contains(js, "✖ boom") {
		t.Fatalf("JSON view must expand JSON cells and still show errors: %s", js)
	}
}

func TestLogViewColorsLevelsAndExpandsJSONLines(t *testing.T) {
	logs := []backend.LogLine{{Time: "2026-09-12 15:23:43.000", Labels: syntax.Object{{Key: "app", Value: "api"}, {Key: "level", Value: "error"}}, Line: `{"msg":"boom"}`}}
	out := RenderResults([]backend.Result{{Title: "{app=\"api\"}", Logs: logs}}, nil, false)
	if !strings.Contains(out, "["+theme.Red+"]ERROR") || !strings.Contains(out, "app=api") || !strings.Contains(out, `"msg"`) {
		t.Fatalf("got %s", out)
	}
}

func TestMaskTargetHidesPasswords(t *testing.T) {
	if got := MaskTarget("postgres://postgres:secret@127.0.0.1:5432/db"); got != "postgres://postgres:***@127.0.0.1:5432/db" {
		t.Fatalf("got %s", got)
	}
	if got := MaskTarget("http://127.0.0.1:3100"); got != "http://127.0.0.1:3100" {
		t.Fatalf("got %s", got)
	}
}

func TestBrailleFillsFromTheBottom(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(2, 2)
	braille(screen, 0, 0, 1, 2, []float64{0.5}, theme.Heat)
	top, _, _, _ := screen.GetContent(0, 0)
	bottom, _, _, _ := screen.GetContent(0, 1)
	if top != ' ' || bottom != '⣿' {
		t.Fatalf("50%% of two rows is one full bottom cell, got %q %q", top, bottom)
	}
}

func testApp() *App {
	return New(Targets{MySQL: "mysql://u:p@127.0.0.1:1/x", Postgres: "postgres://u:p@127.0.0.1:1/x", SQLite: ":memory:", Cassandra: "127.0.0.1:1/x", Redis: "redis://127.0.0.1:1", Loki: "http://127.0.0.1:1", Grafana: "http://127.0.0.1:1", Prometheus: "http://127.0.0.1:1"}, func(func()) {})
}

func TestTypingQInAConsoleDoesNotQuit(t *testing.T) {
	a := testApp()
	a.current = a.TabIndex("mysql")
	if a.keys(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)) == nil {
		t.Fatal("q typed in a SQL editor must reach the editor")
	}
	if a.keys(tcell.NewEventKey(tcell.KeyRune, '2', tcell.ModNone)) == nil {
		t.Fatal("digits typed in a SQL editor must not switch tabs")
	}
}

func TestTabNavigationWraps(t *testing.T) {
	a := testApp()
	a.current = 0
	a.keys(tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone))
	if a.tabs[a.current].Title() != "Prometheus" {
		t.Fatalf("Ctrl-P on the first tab must wrap to the last, got %s", a.tabs[a.current].Title())
	}
	a.keys(tcell.NewEventKey(tcell.KeyF4, 0, tcell.ModNone))
	if a.tabs[a.current].Title() != "Threads" {
		t.Fatalf("F4 must open Threads, got %s", a.tabs[a.current].Title())
	}
}

func TestConfirmDefaultsToCancel(t *testing.T) {
	a := testApp()
	called := false
	a.confirm("kill", "sure?", "SIGKILL", func() { called = true })
	if name, _ := a.root.GetFrontPage(); name != "confirm" {
		t.Fatalf("confirm dialog must be on top, got %s", name)
	}
	a.app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) {})
	if called {
		t.Fatal("Enter on a fresh dialog must cancel, a destructive action needs an explicit choice")
	}
}

func runeKey(r rune, mod tcell.ModMask) *tcell.EventKey {
	return tcell.NewEventKey(tcell.KeyRune, r, mod)
}

func TestCmdKAndCtrlKOpenThePaletteEvenFromAnEditor(t *testing.T) {
	for _, ev := range []*tcell.EventKey{runeKey('k', tcell.ModMeta), tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModCtrl)} {
		a := testApp()
		a.Switch(a.TabIndex("postgres"))
		if a.keys(ev) != nil {
			t.Fatal("the palette shortcut must not reach the editor")
		}
		if front, _ := a.root.GetFrontPage(); front != "palette" {
			t.Fatalf("palette must open, front page is %s", front)
		}
	}
}

func TestCmdDigitGoesToTab(t *testing.T) {
	a := testApp()
	a.Switch(a.TabIndex("mysql"))
	a.keys(runeKey('4', tcell.ModMeta))
	if a.tabs[a.current].Title() != "Threads" {
		t.Fatalf("Cmd-4 must open Threads even while typing, got %s", a.tabs[a.current].Title())
	}
	a.keys(runeKey('0', tcell.ModMeta))
	if a.tabs[a.current].Title() != "Loki" {
		t.Fatalf("Cmd-0 is tab 10, got %s", a.tabs[a.current].Title())
	}
}

func TestPaletteSearchRanksAndEnterNavigates(t *testing.T) {
	a := testApp()
	a.Processes.all = []sys.Process{{PID: 4242, User: "dev", Command: "/usr/bin/postgres-helper"}}
	a.OpenPalette()
	for _, r := range "prom" {
		a.Palette.HandleKey(runeKey(r, tcell.ModNone))
	}
	hits := a.Palette.Hits()
	if len(hits) == 0 || hits[0].Kind != "tab" || hits[0].Title != "Prometheus" {
		t.Fatalf("typing a tab name must put that tab first, got %+v", hits)
	}
	a.Palette.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.tabs[a.current].Title() != "Prometheus" {
		t.Fatalf("Enter must go to the selected tab, got %s", a.tabs[a.current].Title())
	}
	if front, _ := a.root.GetFrontPage(); front != "main" {
		t.Fatalf("palette must close after navigating, front is %s", front)
	}
	a.OpenPalette()
	a.Palette.SetQuery("4242")
	hits = a.Palette.Hits()
	if len(hits) == 0 || hits[0].Kind != "process" {
		t.Fatalf("a pid must find its process, got %+v", hits)
	}
	a.Palette.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.tabs[a.current].Title() != "Processes" || a.Processes.filter.GetText() != "4242" {
		t.Fatal("choosing a process must open Processes filtered to it")
	}
}

func TestPaletteFindsLiveWordsAndInsertsThem(t *testing.T) {
	a := testApp()
	c, idx := a.console("Redis")
	c.editor.SetWords([]string{"session:9f2c", "user:1"})
	a.OpenPalette()
	a.Palette.SetQuery("sess")
	hits := a.Palette.Hits()
	if len(hits) == 0 || hits[0].Title != "session:9f2c" || hits[0].Kind != "redis" {
		t.Fatalf("keys from a connected console must be searchable, got %+v", hits)
	}
	a.Palette.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.current != idx || c.editor.Text() != "session:9f2c" {
		t.Fatalf("Enter must jump to Redis and insert the key, got tab %d text %q", a.current, c.editor.Text())
	}
}

func TestPaletteEscClearsThenCloses(t *testing.T) {
	a := testApp()
	a.OpenPalette()
	a.Palette.SetQuery("zzz")
	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	a.Palette.HandleKey(esc)
	if front, _ := a.root.GetFrontPage(); front != "palette" || a.Palette.Query() != "" {
		t.Fatal("first Esc clears the search")
	}
	a.Palette.HandleKey(esc)
	if front, _ := a.root.GetFrontPage(); front != "main" {
		t.Fatal("second Esc closes the palette")
	}
}

func TestFuzzyPrefersWordStartsAndRejectsMissingLetters(t *testing.T) {
	a, _ := Fuzzy("gr", "Grafana", "")
	b, _ := Fuzzy("gr", "Progress", "")
	if a <= b {
		t.Fatalf("a match at the start of a word must rank higher: %d vs %d", a, b)
	}
	if s, _ := Fuzzy("xyz", "Grafana", "tab"); s >= 0 {
		t.Fatal("missing letters must not match")
	}
	if s, _ := Fuzzy("pg dump", "pg_dump", "process"); s < 0 {
		t.Fatal("every token must be allowed to match separately")
	}
}

func TestShortcutsFilterKeepsWholeGroupOnTitleMatch(t *testing.T) {
	groups, count := FilterShortcuts(shortcutGroups, "containers")
	if len(groups) != 1 || len(groups[0].Rows) != len(shortcutGroups[3].Rows) || count != len(groups[0].Rows) {
		t.Fatalf("matching a group title keeps the whole group, got %+v", groups)
	}
	groups, _ = FilterShortcuts(shortcutGroups, "sigkill")
	if len(groups) != 1 || len(groups[0].Rows) != 1 {
		t.Fatalf("otherwise keep only matching rows, got %+v", groups)
	}
	if _, count := FilterShortcuts(shortcutGroups, "no-such-key"); count != 0 {
		t.Fatal("count must be zero when nothing matches")
	}
}

func TestShortcutsModalFitsSmallScreens(t *testing.T) {
	s := NewShortcuts(func() {})
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(60, 20)
	s.Draw(screen)
	r, _, _, _ := screen.GetContent(58, 0)
	if r == 0 {
		t.Fatal("frame must be drawn inside the screen")
	}
	for range 100 {
		s.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	}
	s.Draw(screen)
	if s.scroll <= 0 {
		t.Fatal("a narrow screen must scroll inside the modal instead of cutting shortcuts off")
	}
}

func TestConnectPromptChecksOnePerKindAndConnectsChosen(t *testing.T) {
	found := []discover.Found{
		{Kind: "Postgres", Container: "a", Target: "postgres://a"},
		{Kind: "Postgres", Container: "b", Target: "postgres://b"},
		{Kind: "Redis", Container: "r", Target: "redis://127.0.0.1:6380/0"},
	}
	var got []discover.Found
	closed := false
	p := NewConnectPrompt(found, func(f []discover.Found) { got = f }, func() { closed = true })
	if c := p.Checked(); len(c) != 2 || c[0].Container != "a" {
		t.Fatalf("first container of each kind is preselected, got %+v", c)
	}
	p.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	p.HandleKey(runeKey(' ', tcell.ModNone))
	if c := p.Checked(); len(c) != 2 || c[0].Container != "b" {
		t.Fatalf("one console holds one connection, checking b must uncheck a, got %+v", c)
	}
	p.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !closed || len(got) != 2 {
		t.Fatalf("Enter connects the checked containers, got %+v", got)
	}
}

func TestConnectFoundRetargetsConsoles(t *testing.T) {
	a := testApp()
	a.ConnectFound([]discover.Found{{Kind: "Loki", Container: "logs", Target: "http://127.0.0.1:3999"}})
	c, _ := a.console("Loki")
	if c.Target() != "http://127.0.0.1:3999" {
		t.Fatalf("got %s", c.Target())
	}
}

func TestBannerLettersLineUp(t *testing.T) {
	lines := BannerLines()
	for _, l := range lines {
		if runewidth.StringWidth(l) != runewidth.StringWidth(lines[0]) {
			t.Fatalf("every banner row must have the same width or the letters shear: %q", l)
		}
	}
}

func TestPaletteRanksNavigationAboveNoise(t *testing.T) {
	a := testApp()
	a.SetFound([]discover.Found{{Kind: "Postgres", Container: "devcli-postgres", Target: "postgres://x"}})
	a.Processes.all = []sys.Process{{PID: 9, Command: "/usr/libexec/postersyncd"}}
	c, _ := a.console("Prometheus")
	c.editor.SetWords([]string{"scrape_samples_post_metric_relabeling"})
	a.OpenPalette()
	a.Palette.SetQuery("post")
	hits := a.Palette.Hits()
	rank := map[string]int{}
	for i, h := range hits {
		if _, seen := rank[h.Kind]; !seen {
			rank[h.Kind] = i
		}
	}
	if rank["tab"] != 0 || rank["connect"] > rank["process"] || rank["connect"] > rank["prometheus"] {
		t.Fatalf("tabs and connect actions are what a user navigates to, got order %+v", hits)
	}
}

func TestObservabilityConsolesOfferTheAllCatalog(t *testing.T) {
	a := testApp()
	for name, want := range map[string]bool{"Loki": true, "Grafana": true, "Prometheus": true, "Postgres": false} {
		c, i := a.console(name)
		if c.HasCatalog() != want {
			t.Fatalf("%s catalog = %v", name, c.HasCatalog())
		}
		a.Switch(i)
		a.OpenPalette()
		a.Palette.SetQuery("list everything")
		hits := a.Palette.Hits()
		found := len(hits) > 0 && hits[0].Title == "List everything available (all)"
		if found != want {
			t.Fatalf("%s: palette all action present = %v", name, found)
		}
		a.closeOverlay("palette")
	}
}

func TestReadyQueriesFromAllLoadIntoTheEditorFromThePalette(t *testing.T) {
	a := testApp()
	c, i := a.console("Prometheus")
	a.Switch(i)
	c.runDone([]backend.Result{{Title: "ready queries", Columns: []string{"query", "what it shows"}, Rows: [][]any{{"sum by (job) (up)", "healthy targets per job"}}}}, nil, 0)
	a.OpenPalette()
	hits := a.Palette.Hits()
	var ready *PaletteItem
	for k := range hits {
		if hits[k].Kind == "ready" {
			ready = &hits[k]
		}
	}
	if ready == nil || ready.Title != "sum by (job) (up)" {
		t.Fatalf("after all, the current console's ready queries must be in the empty palette, got %+v", hits)
	}
	a.Switch(0)
	a.OpenPalette()
	a.Palette.SetQuery("healthy targets")
	hits = a.Palette.Hits()
	if len(hits) == 0 || hits[0].Kind != "ready" {
		t.Fatalf("ready queries must be searchable by what they show, got %+v", hits)
	}
	a.Palette.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.current != i || c.editor.Text() != "sum by (job) (up)" {
		t.Fatalf("Enter must load the query into its console, got tab %d text %q", a.current, c.editor.Text())
	}
}

func drawn(a *App) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(170, 50)
	a.root.SetRect(0, 0, 170, 50)
	a.root.Draw(screen)
}

func clickTab(a *App, title string) {
	i := a.TabIndex(title)
	x := a.bar.ranges[i][0] + 1
	for _, action := range []tview.MouseAction{tview.MouseLeftDown, tview.MouseLeftUp, tview.MouseLeftClick} {
		a.root.MouseHandler()(action, tcell.NewEventMouse(x, 0, tcell.Button1, tcell.ModNone), func(p tview.Primitive) { a.app.SetFocus(p) })
	}
}

func sendKey(a *App, ev *tcell.EventKey) {
	if a.keys(ev) != nil {
		a.root.InputHandler()(ev, func(p tview.Primitive) { a.app.SetFocus(p) })
	}
}

func TestClickingATabBehindADialogCannotStrandTheDialog(t *testing.T) {
	a := testApp()
	a.Switch(0)
	a.SetFound([]discover.Found{{Kind: "Redis", Container: "cache", Target: "redis://127.0.0.1:6380/0"}})
	a.promptFound()
	drawn(a)
	clickTab(a, "Threads")
	if front, _ := a.root.GetFrontPage(); front != "connect" || a.tabs[a.current].Title() != "Dashboard" {
		t.Fatalf("a click behind an open dialog must not reach the tabs, front=%s tab=%s", front, a.tabs[a.current].Title())
	}
	sendKey(a, tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if front, _ := a.root.GetFrontPage(); front != "main" {
		t.Fatalf("Esc must still close the dialog after a stray click, front=%s", front)
	}
	clickTab(a, "Threads")
	if a.tabs[a.current].Title() != "Threads" {
		t.Fatal("with no dialog open a tab click must switch tabs")
	}
}

func TestKeysReachTheDialogEvenIfFocusWandered(t *testing.T) {
	a := testApp()
	a.SetFound([]discover.Found{{Kind: "Redis", Container: "cache", Target: "redis://127.0.0.1:6380/0"}})
	a.promptFound()
	a.app.SetFocus(a.Threads.table)
	sendKey(a, runeKey('n', tcell.ModNone))
	if front, _ := a.root.GetFrontPage(); front != "main" {
		t.Fatalf("a visible dialog must always receive the keyboard, front=%s", front)
	}
}

func TestConfirmDialogSwallowsClicksOutsideIt(t *testing.T) {
	a := testApp()
	a.Switch(0)
	confirmed := false
	a.confirm("kill", "sure?", "SIGKILL", func() { confirmed = true })
	drawn(a)
	clickTab(a, "Processes")
	if front, _ := a.root.GetFrontPage(); front != "confirm" || a.tabs[a.current].Title() != "Dashboard" {
		t.Fatalf("clicks outside a confirm dialog must not switch tabs, front=%s tab=%s", front, a.tabs[a.current].Title())
	}
	sendKey(a, tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if front, _ := a.root.GetFrontPage(); front != "main" || confirmed {
		t.Fatal("Esc must cancel the confirm dialog")
	}
}

func TestLateContainerScanDoesNotPopADialogOverAnActiveUser(t *testing.T) {
	a := testApp()
	a.Switch(a.TabIndex("threads"))
	sendKey(a, tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	a.scanning = false
	a.SetFound([]discover.Found{{Kind: "Redis", Container: "cache", Target: "redis://127.0.0.1:6380/0"}})
	a.CloseSplash()
	a.splashOn.Store(true)
	a.CloseSplash()
	if front, _ := a.root.GetFrontPage(); front != "main" {
		t.Fatalf("once the user is working, a scan result must not steal the screen, front=%s", front)
	}
	a.OpenPalette()
	a.Palette.SetQuery("connect redis")
	if hits := a.Palette.Hits(); len(hits) == 0 || hits[0].Kind != "connect" {
		t.Fatalf("the found containers must stay reachable from Cmd-K, got %+v", hits)
	}
}
