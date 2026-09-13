package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

const paletteLimit = 200

type PaletteItem struct {
	Kind   string
	Title  string
	Detail string
	Run    func()
}

type paletteHit struct {
	item      PaletteItem
	score     int
	highlight map[int]bool
}

type Palette struct {
	*tview.Box
	query   []rune
	source  func(query string) []PaletteItem
	hits    []paletteHit
	index   int
	top     int
	rows    int
	listY   int
	onClose func()
}

var kindColors = map[string]string{
	"tab":       theme.Cyan,
	"action":    theme.Magenta,
	"connect":   theme.Lime,
	"ready":     theme.Orange,
	"container": theme.Blue,
	"jvm":       theme.Orange,
	"process":   theme.Purple,
	"history":   theme.Dim,
}

var kindWeights = map[string]int{
	"tab":       60,
	"connect":   50,
	"ready":     45,
	"action":    40,
	"container": 30,
	"jvm":       30,
	"history":   10,
	"process":   0,
}

func kindWeight(kind string) int {
	if w, ok := kindWeights[kind]; ok {
		return w
	}
	return 15
}

func kindColor(kind string) string {
	if c, ok := kindColors[kind]; ok {
		return c
	}
	return theme.Yellow
}

func NewPalette(source func(query string) []PaletteItem, onClose func()) *Palette {
	p := &Palette{Box: tview.NewBox(), source: source, onClose: onClose}
	p.refresh()
	return p
}

func (p *Palette) Query() string { return string(p.query) }

func (p *Palette) Hits() []PaletteItem {
	out := make([]PaletteItem, len(p.hits))
	for i, h := range p.hits {
		out[i] = h.item
	}
	return out
}

func (p *Palette) SetQuery(q string) {
	p.query = []rune(q)
	p.refresh()
}

func (p *Palette) refresh() {
	q := string(p.query)
	p.hits = p.hits[:0]
	for i, item := range p.source(q) {
		if strings.TrimSpace(q) == "" {
			p.hits = append(p.hits, paletteHit{item: item, score: -i})
			continue
		}
		score, positions := Fuzzy(q, item.Title, item.Kind+" "+item.Detail)
		if score < 0 {
			continue
		}
		score += kindWeight(item.Kind)
		hl := map[int]bool{}
		for _, pos := range positions {
			hl[pos] = true
		}
		p.hits = append(p.hits, paletteHit{item: item, score: score, highlight: hl})
	}
	sort.SliceStable(p.hits, func(i, j int) bool { return p.hits[i].score > p.hits[j].score })
	if len(p.hits) > paletteLimit {
		p.hits = p.hits[:paletteLimit]
	}
	p.index, p.top = 0, 0
}

func (p *Palette) move(delta int) {
	if len(p.hits) == 0 {
		return
	}
	p.index = max(0, min(len(p.hits)-1, p.index+delta))
}

func (p *Palette) activate() {
	if len(p.hits) == 0 {
		return
	}
	item := p.hits[p.index].item
	p.onClose()
	if item.Run != nil {
		item.Run()
	}
}

func (p *Palette) HandleKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyRune:
		p.query = append(p.query, event.Rune())
		p.refresh()
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			p.refresh()
		}
	case tcell.KeyCtrlU:
		p.SetQuery("")
	case tcell.KeyEscape:
		if len(p.query) > 0 {
			p.SetQuery("")
			return
		}
		p.onClose()
	case tcell.KeyCtrlK:
		p.onClose()
	case tcell.KeyEnter:
		p.activate()
	case tcell.KeyUp, tcell.KeyCtrlP:
		p.move(-1)
	case tcell.KeyDown, tcell.KeyCtrlN, tcell.KeyTab:
		p.move(1)
	case tcell.KeyPgUp:
		p.move(-max(1, p.rows))
	case tcell.KeyPgDn:
		p.move(max(1, p.rows))
	case tcell.KeyHome:
		p.index = 0
	case tcell.KeyEnd:
		p.move(len(p.hits))
	}
}

func (p *Palette) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return p.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		p.HandleKey(event)
	})
}

func (p *Palette) PasteHandler() func(text string, setFocus func(p tview.Primitive)) {
	return p.WrapPasteHandler(func(text string, setFocus func(p tview.Primitive)) {
		p.query = append(p.query, []rune(strings.ReplaceAll(text, "\n", " "))...)
		p.refresh()
	})
}

func (p *Palette) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return p.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		_, y := event.Position()
		switch action {
		case tview.MouseScrollUp:
			p.move(-3)
			return true, nil
		case tview.MouseScrollDown:
			p.move(3)
			return true, nil
		case tview.MouseLeftClick:
			row := p.top + y - p.listY
			if y >= p.listY && row >= 0 && row < len(p.hits) {
				p.index = row
				p.activate()
			}
			return true, nil
		}
		return true, nil
	})
}

func modalRect(screen tcell.Screen, maxW, maxH int) (int, int, int, int) {
	sw, sh := screen.Size()
	w := min(maxW, sw-4)
	h := min(maxH, sh-2)
	return (sw - w) / 2, max(1, (sh-h)/3), w, h
}

func drawFrame(screen tcell.Screen, x, y, w, h int, color, title string) {
	bg := tcell.StyleDefault.Background(theme.Color("#121a33"))
	fill(screen, x, y, w, h, bg)
	border := bg.Foreground(theme.Color(color))
	for i := x; i < x+w; i++ {
		screen.SetContent(i, y, '━', nil, border)
		screen.SetContent(i, y+h-1, '━', nil, border)
	}
	for j := y; j < y+h; j++ {
		screen.SetContent(x, j, '┃', nil, border)
		screen.SetContent(x+w-1, j, '┃', nil, border)
	}
	screen.SetContent(x, y, '┏', nil, border)
	screen.SetContent(x+w-1, y, '┓', nil, border)
	screen.SetContent(x, y+h-1, '┗', nil, border)
	screen.SetContent(x+w-1, y+h-1, '┛', nil, border)
	put(screen, x+2, y, x+w-2, title, border.Bold(true))
}

func (p *Palette) Draw(screen tcell.Screen) {
	sw, sh := screen.Size()
	p.SetRect(0, 0, sw, sh)
	x, y, w, h := modalRect(screen, 100, 24)
	drawFrame(screen, x, y, w, h, theme.Magenta, " ⌘K  search anything ")
	bg := tcell.StyleDefault.Background(theme.Color("#121a33"))
	inner := x + 2
	right := x + w - 2
	cx := put(screen, inner, y+1, right, "❯ ", bg.Foreground(theme.Color(theme.Magenta)).Bold(true))
	if len(p.query) == 0 {
		put(screen, cx, y+1, right, "tabs, commands, containers, jvms, processes, tables, keys, history…", bg.Foreground(theme.Color(theme.Dim)))
		screen.ShowCursor(cx, y+1)
	} else {
		end := put(screen, cx, y+1, right, string(p.query), bg.Foreground(theme.Color(theme.Text)).Bold(true))
		screen.ShowCursor(end, y+1)
	}
	for i := x + 1; i < x+w-1; i++ {
		screen.SetContent(i, y+2, '─', nil, bg.Foreground(theme.Color(theme.Border)))
	}
	p.listY = y + 3
	p.rows = h - 5
	if p.index < p.top {
		p.top = p.index
	}
	if p.index >= p.top+p.rows {
		p.top = p.index - p.rows + 1
	}
	if len(p.hits) == 0 {
		put(screen, inner, p.listY, right, "nothing matches "+string(p.query), bg.Foreground(theme.Color(theme.Dim)))
	}
	for row := 0; row < p.rows && p.top+row < len(p.hits); row++ {
		i := p.top + row
		hit := p.hits[i]
		ry := p.listY + row
		st := bg
		if i == p.index {
			st = tcell.StyleDefault.Background(theme.Color("#3a1450"))
			fill(screen, x+1, ry, w-2, 1, st)
			put(screen, x+1, ry, right, "▌", st.Foreground(theme.Color(theme.Magenta)))
		}
		badge := fmt.Sprintf("%-10s", truncate(hit.item.Kind, 10))
		bx := put(screen, inner, ry, right, badge, st.Foreground(theme.Color(kindColor(hit.item.Kind))))
		titleMax := min(right, bx+1+max(20, (right-bx)*3/5))
		tx := bx + 1
		for k, r := range []rune(hit.item.Title) {
			rw := max(1, runewidth.RuneWidth(r))
			if tx+rw > titleMax {
				break
			}
			rs := st.Foreground(theme.Color(theme.Text))
			if hit.highlight[k] {
				rs = st.Foreground(theme.Color(theme.Cyan)).Bold(true).Underline(true)
			}
			screen.SetContent(tx, ry, r, nil, rs)
			tx += rw
		}
		detail := truncate(hit.item.Detail, max(0, right-tx-2))
		put(screen, right-runewidth.StringWidth(detail), ry, right, detail, st.Foreground(theme.Color(theme.Dim)))
	}
	footer := fmt.Sprintf(" %d results · ↑↓ move · Enter go · Esc clear/close · Ctrl-K close ", len(p.hits))
	put(screen, x+w-2-runewidth.StringWidth(footer), y+h-1, x+w-1, footer, bg.Foreground(theme.Color(theme.Dim)))
}
