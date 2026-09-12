package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/diegopacheco/dev-cli/internal/backend"
	"github.com/diegopacheco/dev-cli/internal/syntax"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
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
	key(e, tcell.KeyCtrlK)
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
	if got := maskTarget("postgres://postgres:secret@127.0.0.1:5432/db"); got != "postgres://postgres:***@127.0.0.1:5432/db" {
		t.Fatalf("got %s", got)
	}
	if got := maskTarget("http://127.0.0.1:3100"); got != "http://127.0.0.1:3100" {
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
	return New(Targets{MySQL: "mysql://u:p@127.0.0.1:1/x", Postgres: "postgres://u:p@127.0.0.1:1/x", SQLite: ":memory:", Cassandra: "127.0.0.1:1/x", Redis: "redis://127.0.0.1:1", Loki: "http://127.0.0.1:1", Grafana: "http://127.0.0.1:1"}, func(func()) {})
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
	if a.tabs[a.current].Title() != "Grafana" {
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
