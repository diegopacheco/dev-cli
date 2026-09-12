package ui

import (
	"fmt"
	"strings"

	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

type Shortcut struct {
	Keys string
	Desc string
}

type ShortcutGroup struct {
	Name  string
	Icon  string
	Color string
	Rows  []Shortcut
}

var shortcutGroups = []ShortcutGroup{
	{"Navigation", "◆", theme.Cyan, []Shortcut{
		{"Cmd-K / Ctrl-K", "search anything, Enter goes there"},
		{"Cmd-/ / Ctrl-/", "show all shortcuts"},
		{"Cmd-1..9, Cmd-0", "go to tab 1..9, 10"},
		{"F1..F12", "go to tab 1..12"},
		{"Ctrl-N / Ctrl-P", "next / previous tab"},
		{"1..9", "go to tab (outside editors)"},
		{"click tab", "go to tab"},
		{"q / Ctrl-Q", "quit"},
	}},
	{"Editor", "✎", theme.Magenta, []Shortcut{
		{"Ctrl-R", "run the buffer"},
		{"Enter", "run (SQL/CQL after ;) or newline"},
		{"Tab", "accept completion"},
		{"↑ ↓", "move in popup or history"},
		{"Ctrl-L", "clear the buffer"},
		{"Ctrl-T", "table / JSON results"},
		{"Ctrl-E", "edit connection"},
		{"F5", "reload completion words"},
		{"PgUp / PgDn", "scroll results"},
		{"Esc", "cancel a running query"},
	}},
	{"Processes", "☰", theme.Purple, []Shortcut{
		{"/", "filter by text or pid"},
		{"o", "sort CPU / MEM / PID"},
		{"x", "SIGTERM"},
		{"K", "SIGKILL"},
		{"r", "refresh"},
		{"Esc", "clear filter"},
	}},
	{"Containers", "⬢", theme.Blue, []Shortcut{
		{"Enter / e", "shell into container"},
		{"s / S", "stop / start"},
		{"k", "kill"},
		{"d", "remove"},
		{"r", "refresh"},
	}},
	{"Threads", "☕", theme.Orange, []Shortcut{
		{"Enter", "thread dump"},
		{"Tab", "focus dump"},
		{"/", "filter threads"},
		{"s", "cycle state filter"},
		{"r", "refresh jvms"},
	}},
	{"Dialogs", "⚠", theme.Red, []Shortcut{
		{"y", "confirm"},
		{"n / Esc", "cancel"},
		{"Space", "toggle a discovered container"},
		{"a", "toggle all discovered"},
	}},
}

type Shortcuts struct {
	*tview.Box
	query   []rune
	scroll  int
	onClose func()
}

func NewShortcuts(onClose func()) *Shortcuts {
	return &Shortcuts{Box: tview.NewBox(), onClose: onClose}
}

func (s *Shortcuts) SetQuery(q string) {
	s.query = []rune(q)
	s.scroll = 0
}

func FilterShortcuts(groups []ShortcutGroup, query string) ([]ShortcutGroup, int) {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []ShortcutGroup
	count := 0
	for _, g := range groups {
		if q == "" || strings.Contains(strings.ToLower(g.Name), q) {
			out = append(out, g)
			count += len(g.Rows)
			continue
		}
		kept := ShortcutGroup{Name: g.Name, Icon: g.Icon, Color: g.Color}
		for _, r := range g.Rows {
			if strings.Contains(strings.ToLower(r.Keys), q) || strings.Contains(strings.ToLower(r.Desc), q) {
				kept.Rows = append(kept.Rows, r)
			}
		}
		if len(kept.Rows) > 0 {
			out = append(out, kept)
			count += len(kept.Rows)
		}
	}
	return out, count
}

func (s *Shortcuts) HandleKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyRune:
		if event.Rune() == '/' && event.Modifiers()&(tcell.ModMeta|tcell.ModCtrl) != 0 {
			s.onClose()
			return
		}
		s.query = append(s.query, event.Rune())
		s.scroll = 0
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(s.query) > 0 {
			s.query = s.query[:len(s.query)-1]
		}
	case tcell.KeyEscape:
		if len(s.query) > 0 {
			s.SetQuery("")
			return
		}
		s.onClose()
	case tcell.KeyCtrlUnderscore, tcell.KeyEnter:
		s.onClose()
	case tcell.KeyUp:
		s.scroll = max(0, s.scroll-1)
	case tcell.KeyDown:
		s.scroll++
	case tcell.KeyPgUp:
		s.scroll = max(0, s.scroll-10)
	case tcell.KeyPgDn:
		s.scroll += 10
	}
}

func (s *Shortcuts) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return s.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		s.HandleKey(event)
	})
}

func (s *Shortcuts) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return s.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		switch action {
		case tview.MouseScrollUp:
			s.scroll = max(0, s.scroll-2)
		case tview.MouseScrollDown:
			s.scroll += 2
		}
		return true, nil
	})
}

type placed struct {
	group ShortcutGroup
	col   int
	row   int
}

func layoutGroups(groups []ShortcutGroup, cols int) ([]placed, int) {
	heights := make([]int, cols)
	var out []placed
	for _, g := range groups {
		c := 0
		for i := range heights {
			if heights[i] < heights[c] {
				c = i
			}
		}
		out = append(out, placed{g, c, heights[c]})
		heights[c] += len(g.Rows) + 3
	}
	total := 0
	for _, h := range heights {
		total = max(total, h)
	}
	return out, total
}

func (s *Shortcuts) Draw(screen tcell.Screen) {
	sw, sh := screen.Size()
	s.SetRect(0, 0, sw, sh)
	x, y, w, h := modalRect(screen, 150, sh*92/100)
	drawFrame(screen, x, y, w, h, theme.Cyan, " ⌘/  shortcuts ")
	bg := tcell.StyleDefault.Background(theme.Color("#121a33"))
	right := x + w - 2
	cx := put(screen, x+2, y+1, right, "⌕ ", bg.Foreground(theme.Color(theme.Cyan)).Bold(true))
	groups, count := FilterShortcuts(shortcutGroups, string(s.query))
	if len(s.query) == 0 {
		put(screen, cx, y+1, right, "filter shortcuts…", bg.Foreground(theme.Color(theme.Dim)))
		screen.ShowCursor(cx, y+1)
	} else {
		screen.ShowCursor(put(screen, cx, y+1, right, string(s.query), bg.Foreground(theme.Color(theme.Text)).Bold(true)), y+1)
	}
	countText := fmt.Sprintf("%d shortcuts", count)
	put(screen, right-runewidth.StringWidth(countText), y+1, right, countText, bg.Foreground(theme.Color(theme.Dim)))
	for i := x + 1; i < x+w-1; i++ {
		screen.SetContent(i, y+2, '─', nil, bg.Foreground(theme.Color(theme.Border)))
	}
	bodyY, bodyH := y+3, h-4
	if count == 0 {
		put(screen, x+2, bodyY+1, right, "no shortcut matches \""+string(s.query)+"\"", bg.Foreground(theme.Color(theme.Dim)))
		return
	}
	cols := 1
	if w >= 110 {
		cols = 3
	} else if w >= 72 {
		cols = 2
	}
	colW := (w - 4) / cols
	layout, total := layoutGroups(groups, cols)
	s.scroll = max(0, min(s.scroll, total-bodyH))
	for _, pl := range layout {
		gx := x + 2 + pl.col*colW
		gRight := gx + colW - 2
		line := func(offset int, draw func(ry int)) {
			ry := bodyY + pl.row + offset - s.scroll
			if ry >= bodyY && ry < bodyY+bodyH {
				draw(ry)
			}
		}
		gst := bg.Foreground(theme.Color(pl.group.Color))
		line(0, func(ry int) {
			end := put(screen, gx, ry, gRight, pl.group.Icon+" "+strings.ToUpper(pl.group.Name)+" ", gst.Bold(true))
			for i := end; i < gRight; i++ {
				screen.SetContent(i, ry, '─', nil, gst.Dim(true))
			}
		})
		keyW := 0
		for _, r := range pl.group.Rows {
			keyW = max(keyW, runewidth.StringWidth(r.Keys))
		}
		keyW = min(keyW, colW/2)
		for i, r := range pl.group.Rows {
			line(i+1, func(ry int) {
				screen.SetContent(gx, ry, '│', nil, gst.Dim(true))
				put(screen, gx+2, ry, gx+2+keyW, r.Keys, gst.Bold(true))
				put(screen, gx+3+keyW, ry, gRight, r.Desc, bg.Foreground(theme.Color(theme.Text)))
			})
		}
	}
	if total > bodyH {
		hint := " ↑↓ scroll "
		put(screen, x+w-2-len(hint), y+h-1, x+w-1, hint, bg.Foreground(theme.Color(theme.Dim)))
	}
	put(screen, x+2, y+h-1, right, " Esc clears the search, Esc again closes ", bg.Foreground(theme.Color(theme.Dim)))
}
