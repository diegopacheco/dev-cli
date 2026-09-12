package ui

import (
	"fmt"

	"github.com/diegopacheco/dev-cli/internal/discover"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

type ConnectPrompt struct {
	*tview.Box
	found     []discover.Found
	checked   []bool
	index     int
	onConnect func([]discover.Found)
	onClose   func()
}

func NewConnectPrompt(found []discover.Found, onConnect func([]discover.Found), onClose func()) *ConnectPrompt {
	p := &ConnectPrompt{Box: tview.NewBox(), found: found, checked: make([]bool, len(found)), onConnect: onConnect, onClose: onClose}
	seen := map[string]bool{}
	for i, f := range found {
		if !seen[f.Kind] {
			seen[f.Kind] = true
			p.checked[i] = true
		}
	}
	return p
}

func (p *ConnectPrompt) Checked() []discover.Found {
	var out []discover.Found
	for i, f := range p.found {
		if p.checked[i] {
			out = append(out, f)
		}
	}
	return out
}

func (p *ConnectPrompt) toggle(i int) {
	p.checked[i] = !p.checked[i]
	if !p.checked[i] {
		return
	}
	for j, f := range p.found {
		if j != i && f.Kind == p.found[i].Kind {
			p.checked[j] = false
		}
	}
}

func (p *ConnectPrompt) HandleKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyUp:
		p.index = max(0, p.index-1)
	case tcell.KeyDown, tcell.KeyTab:
		p.index = min(len(p.found)-1, p.index+1)
	case tcell.KeyEnter:
		chosen := p.Checked()
		p.onClose()
		p.onConnect(chosen)
	case tcell.KeyEscape:
		p.onClose()
	case tcell.KeyRune:
		switch event.Rune() {
		case ' ':
			p.toggle(p.index)
		case 'a':
			all := true
			for _, c := range p.checked {
				all = all && c
			}
			for i := range p.checked {
				p.checked[i] = false
			}
			if !all {
				seen := map[string]bool{}
				for i, f := range p.found {
					if !seen[f.Kind] {
						seen[f.Kind] = true
						p.checked[i] = true
					}
				}
			}
		case 'n', 'q':
			p.onClose()
		case 'y':
			chosen := p.Checked()
			p.onClose()
			p.onConnect(chosen)
		case 'j':
			p.index = min(len(p.found)-1, p.index+1)
		case 'k':
			p.index = max(0, p.index-1)
		}
	}
}

func (p *ConnectPrompt) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return p.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		p.HandleKey(event)
	})
}

func (p *ConnectPrompt) Draw(screen tcell.Screen) {
	sw, sh := screen.Size()
	p.SetRect(0, 0, sw, sh)
	x, y, w, h := modalRect(screen, 120, len(p.found)+8)
	drawFrame(screen, x, y, w, h, theme.Lime, fmt.Sprintf(" ⬢ found %d data containers · connect? ", len(p.found)))
	bg := tcell.StyleDefault.Background(theme.Color("#121a33"))
	right := x + w - 2
	put(screen, x+2, y+1, right, "devcli found these running containers. Pick the ones each console should connect to.", bg.Foreground(theme.Color(theme.Text)))
	for i, f := range p.found {
		ry := y + 3 + i
		st := bg
		if i == p.index {
			st = tcell.StyleDefault.Background(theme.Color("#16341f"))
			fill(screen, x+1, ry, w-2, 1, st)
		}
		box, boxColor := "[ ]", theme.Dim
		if p.checked[i] {
			box, boxColor = "[✔]", theme.Lime
		}
		cx := put(screen, x+2, ry, right, box+" ", st.Foreground(theme.Color(boxColor)).Bold(true))
		cx = put(screen, cx, ry, right, fmt.Sprintf("%-11s", f.Kind), st.Foreground(theme.Color(theme.Cyan)).Bold(true))
		cx = put(screen, cx, ry, right, fmt.Sprintf("%-22s", truncate(f.Container, 21)), st.Foreground(theme.Color(theme.Magenta)))
		put(screen, cx, ry, right, truncate(MaskTarget(f.Target), right-cx), st.Foreground(theme.Color(theme.Text)))
	}
	footer := " Space toggle · a all · Enter/y connect · Esc/n skip "
	put(screen, x+w-2-runewidth.StringWidth(footer), y+h-1, x+w-1, footer, bg.Foreground(theme.Color(theme.Dim)))
}
