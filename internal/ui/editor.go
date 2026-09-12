package ui

import (
	"strconv"
	"strings"

	"github.com/diegopacheco/dev-cli/internal/syntax"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

const popupSize = 8

var tokenColors = map[syntax.Kind]string{
	syntax.Plain:    theme.Text,
	syntax.Keyword:  theme.Cyan,
	syntax.Function: theme.Magenta,
	syntax.String:   theme.Lime,
	syntax.Number:   theme.Orange,
	syntax.Comment:  theme.Dim,
	syntax.Operator: theme.Yellow,
	syntax.Variable: theme.Purple,
	syntax.Label:    theme.Purple,
	syntax.Punct:    theme.Dim,
}

type Editor struct {
	*tview.Box
	lines       [][]rune
	row, col    int
	top, left   int
	lang        syntax.Language
	words       []string
	popup       []string
	popupIndex  int
	history     []string
	historyAt   int
	draft       string
	onRun       func(text string)
	runsOnEnter func(text string) bool
}

func NewEditor(lang syntax.Language) *Editor {
	e := &Editor{Box: tview.NewBox(), lines: [][]rune{{}}, lang: lang}
	e.SetBackgroundColor(theme.Color(theme.Bg))
	return e
}

func (e *Editor) SetWords(words []string) {
	e.words = words
}

func (e *Editor) Text() string {
	parts := make([]string, len(e.lines))
	for i, l := range e.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

func (e *Editor) SetText(text string) {
	e.lines = nil
	for _, l := range strings.Split(text, "\n") {
		e.lines = append(e.lines, []rune(l))
	}
	e.row = len(e.lines) - 1
	e.col = len(e.lines[e.row])
	e.popup = nil
}

func (e *Editor) Cursor() (int, int) {
	return e.row, e.col
}

func (e *Editor) History() []string {
	return e.history
}

func (e *Editor) Words() []string {
	return e.words
}

func (e *Editor) Insert(text string) {
	e.insert(text)
	e.popup = nil
}

func (e *Editor) Popup() []string {
	return e.popup
}

func (e *Editor) candidates() []string {
	all := make([]string, 0, len(e.words)+64)
	for _, w := range e.lang.Words() {
		if !e.lang.CaseFold {
			w = strings.ToLower(w)
		}
		all = append(all, w)
	}
	return append(all, e.words...)
}

func (e *Editor) refreshPopup() {
	prefix, _ := syntax.WordBefore(e.lines[e.row], e.col)
	if prefix == "" || strings.TrimLeft(prefix, "0123456789.-") == "" {
		e.popup = nil
		return
	}
	e.popup = syntax.Complete(prefix, e.candidates(), popupSize)
	e.popupIndex = 0
}

func (e *Editor) accept() {
	if len(e.popup) == 0 {
		return
	}
	line := e.lines[e.row]
	prefix, start := syntax.WordBefore(line, e.col)
	word := []rune(syntax.MatchCase(prefix, e.popup[e.popupIndex], e.lang))
	rest := append([]rune{}, line[e.col:]...)
	e.lines[e.row] = append(append(append([]rune{}, line[:start]...), word...), rest...)
	e.col = start + len(word)
	e.popup = nil
}

func (e *Editor) insert(text string) {
	for _, r := range text {
		if r == '\n' {
			e.newline(false)
			continue
		}
		if r == '\r' {
			continue
		}
		if r == '\t' {
			r = ' '
		}
		line := e.lines[e.row]
		line = append(line[:e.col], append([]rune{r}, line[e.col:]...)...)
		e.lines[e.row] = line
		e.col++
	}
}

func (e *Editor) newline(indent bool) {
	line := e.lines[e.row]
	head := append([]rune{}, line[:e.col]...)
	tail := append([]rune{}, line[e.col:]...)
	prefix := []rune{}
	if indent {
		for _, r := range head {
			if r != ' ' {
				break
			}
			prefix = append(prefix, r)
		}
	}
	e.lines[e.row] = head
	rest := append([][]rune{append(prefix, tail...)}, e.lines[e.row+1:]...)
	e.lines = append(e.lines[:e.row+1], rest...)
	e.row++
	e.col = len(prefix)
}

func (e *Editor) backspace() {
	if e.col > 0 {
		line := e.lines[e.row]
		e.lines[e.row] = append(line[:e.col-1], line[e.col:]...)
		e.col--
		return
	}
	if e.row == 0 {
		return
	}
	prev := e.lines[e.row-1]
	e.col = len(prev)
	e.lines[e.row-1] = append(prev, e.lines[e.row]...)
	e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
	e.row--
}

func (e *Editor) deleteForward() {
	line := e.lines[e.row]
	if e.col < len(line) {
		e.lines[e.row] = append(line[:e.col], line[e.col+1:]...)
		return
	}
	if e.row < len(e.lines)-1 {
		e.lines[e.row] = append(line, e.lines[e.row+1]...)
		e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
	}
}

func (e *Editor) run() {
	text := strings.TrimSpace(e.Text())
	if text == "" || e.onRun == nil {
		return
	}
	if len(e.history) == 0 || e.history[len(e.history)-1] != text {
		e.history = append(e.history, text)
	}
	e.historyAt = len(e.history)
	e.popup = nil
	e.onRun(text)
}

func (e *Editor) recall(delta int) {
	if len(e.history) == 0 {
		return
	}
	next := e.historyAt + delta
	if next < 0 || next > len(e.history) {
		return
	}
	if e.historyAt == len(e.history) {
		e.draft = e.Text()
	}
	e.historyAt = next
	if next == len(e.history) {
		e.SetText(e.draft)
		return
	}
	e.SetText(e.history[next])
}

func (e *Editor) HandleKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyRune:
		e.insert(string(event.Rune()))
		e.refreshPopup()
		return
	case tcell.KeyEnter:
		e.popup = nil
		if e.runsOnEnter != nil && e.runsOnEnter(e.Text()) {
			e.run()
			return
		}
		e.newline(true)
	case tcell.KeyCtrlR:
		e.run()
	case tcell.KeyTab:
		if len(e.popup) > 0 {
			e.accept()
			return
		}
		e.insert("  ")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		e.backspace()
		e.refreshPopup()
		return
	case tcell.KeyDelete:
		e.deleteForward()
	case tcell.KeyLeft:
		if e.col > 0 {
			e.col--
		} else if e.row > 0 {
			e.row--
			e.col = len(e.lines[e.row])
		}
	case tcell.KeyRight:
		if e.col < len(e.lines[e.row]) {
			e.col++
		} else if e.row < len(e.lines)-1 {
			e.row++
			e.col = 0
		}
	case tcell.KeyHome, tcell.KeyCtrlA:
		e.col = 0
	case tcell.KeyEnd:
		e.col = len(e.lines[e.row])
	case tcell.KeyUp:
		if len(e.popup) > 0 {
			e.popupIndex = (e.popupIndex - 1 + len(e.popup)) % len(e.popup)
			return
		}
		if e.row == 0 {
			e.recall(-1)
			return
		}
		e.row--
		e.col = min(e.col, len(e.lines[e.row]))
	case tcell.KeyDown:
		if len(e.popup) > 0 {
			e.popupIndex = (e.popupIndex + 1) % len(e.popup)
			return
		}
		if e.row == len(e.lines)-1 {
			e.recall(1)
			return
		}
		e.row++
		e.col = min(e.col, len(e.lines[e.row]))
	case tcell.KeyEscape:
	case tcell.KeyCtrlL:
		e.SetText("")
	default:
		return
	}
	e.popup = nil
}

func (e *Editor) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return e.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		e.HandleKey(event)
	})
}

func (e *Editor) PasteHandler() func(text string, setFocus func(p tview.Primitive)) {
	return e.WrapPasteHandler(func(text string, setFocus func(p tview.Primitive)) {
		e.insert(text)
		e.popup = nil
	})
}

func (e *Editor) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return e.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		x, y := event.Position()
		if !e.InRect(x, y) {
			return false, nil
		}
		if action == tview.MouseLeftClick {
			setFocus(e)
			ix, iy, _, _ := e.GetInnerRect()
			row := e.top + y - iy
			if row >= 0 && row < len(e.lines) {
				e.row = row
				e.col = max(0, min(len(e.lines[row]), e.left+x-ix-e.gutter()))
				e.popup = nil
			}
			return true, nil
		}
		return false, nil
	})
}

func (e *Editor) gutter() int {
	return len(strconv.Itoa(len(e.lines))) + 2
}

func (e *Editor) Draw(screen tcell.Screen) {
	e.DrawForSubclass(screen, e)
	x, y, w, h := e.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	gutter := e.gutter()
	textW := max(1, w-gutter)
	if e.row < e.top {
		e.top = e.row
	}
	if e.row >= e.top+h {
		e.top = e.row - h + 1
	}
	if e.col < e.left {
		e.left = e.col
	}
	if e.col >= e.left+textW {
		e.left = e.col - textW + 1
	}
	bg := theme.Color(theme.Bg)
	lineBg := theme.Color("#151c33")
	inBlock := false
	for i := 0; i < len(e.lines) && i < e.top+h; i++ {
		tokens, next := syntax.Lex(e.lang, e.lines[i], inBlock)
		wasBlock := inBlock
		inBlock = next
		if i < e.top {
			continue
		}
		rowY := y + i - e.top
		back := bg
		if i == e.row && e.HasFocus() {
			back = lineBg
		}
		fill(screen, x, rowY, w, 1, tcell.StyleDefault.Background(back))
		numColor := theme.Dim
		if i == e.row {
			numColor = theme.Cyan
		}
		num := strconv.Itoa(i + 1)
		put(screen, x+gutter-2-len(num), rowY, x+gutter, num, tcell.StyleDefault.Foreground(theme.Color(numColor)).Background(back).Bold(i == e.row))
		screen.SetContent(x+gutter-1, rowY, '│', nil, tcell.StyleDefault.Foreground(theme.Color(theme.Border)).Background(back))
		colors := make([]string, len(e.lines[i]))
		for c := range colors {
			colors[c] = theme.Text
			if wasBlock {
				colors[c] = theme.Dim
			}
		}
		for _, t := range tokens {
			for c := t.Start; c < t.End && c < len(colors); c++ {
				colors[c] = tokenColors[t.Kind]
			}
		}
		cx := x + gutter
		for c := e.left; c < len(e.lines[i]); c++ {
			r := e.lines[i][c]
			rw := max(1, runewidth.RuneWidth(r))
			if cx+rw > x+w {
				break
			}
			screen.SetContent(cx, rowY, r, nil, tcell.StyleDefault.Foreground(theme.Color(colors[c])).Background(back))
			cx += rw
		}
	}
	for rowY := y + len(e.lines) - e.top; rowY < y+h; rowY++ {
		if rowY >= y {
			screen.SetContent(x+gutter-1, rowY, '│', nil, style(theme.Border))
		}
	}
	cursorX := x + gutter + runewidth.StringWidth(string(e.lines[e.row][e.left:e.col]))
	cursorY := y + e.row - e.top
	if e.HasFocus() {
		screen.ShowCursor(cursorX, cursorY)
	}
	e.drawPopup(screen, x, y, w, h, cursorX, cursorY)
}

func (e *Editor) drawPopup(screen tcell.Screen, x, y, w, h, cx, cy int) {
	if len(e.popup) == 0 {
		return
	}
	width := 0
	for _, p := range e.popup {
		width = max(width, runewidth.StringWidth(p)+4)
	}
	width = min(width, w)
	prefix, _ := syntax.WordBefore(e.lines[e.row], e.col)
	px := max(x, min(cx-runewidth.StringWidth(prefix), x+w-width))
	py := cy + 1
	if py+len(e.popup) > y+h {
		py = cy - len(e.popup)
	}
	py = max(y, py)
	for i, item := range e.popup {
		if py+i >= y+h {
			break
		}
		st := tcell.StyleDefault.Background(theme.Color("#1b2340")).Foreground(theme.Color(theme.Text))
		marker := " "
		if i == e.popupIndex {
			st = tcell.StyleDefault.Background(theme.Color(theme.Magenta)).Foreground(theme.Color(theme.Bg)).Bold(true)
			marker = "▸"
		}
		fill(screen, px, py+i, width, 1, st)
		kind := syntax.Plain
		if toks, _ := syntax.Lex(e.lang, []rune(item), false); len(toks) == 1 && toks[0].End == len([]rune(item)) {
			kind = toks[0].Kind
		}
		if i != e.popupIndex && kind != syntax.Plain {
			st = st.Foreground(theme.Color(tokenColors[kind]))
		}
		put(screen, px, py+i, px+width, marker+" "+item, st)
	}
}
