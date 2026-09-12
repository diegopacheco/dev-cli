package ui

import (
	"math"
	"time"

	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

var letters = [][]string{
	{"██████╗ ", "██╔══██╗", "██║  ██║", "██║  ██║", "██████╔╝", "╚═════╝ "},
	{"███████╗", "██╔════╝", "█████╗  ", "██╔══╝  ", "███████╗", "╚══════╝"},
	{"██╗   ██╗", "██║   ██║", "██║   ██║", "╚██╗ ██╔╝", " ╚████╔╝ ", "  ╚═══╝  "},
	{" ██████╗", "██╔════╝", "██║     ", "██║     ", "╚██████╗", " ╚═════╝"},
	{"██╗     ", "██║     ", "██║     ", "██║     ", "███████╗", "╚══════╝"},
	{"██╗", "██║", "██║", "██║", "██║", "╚═╝"},
}

const Version = "0.2.0"

const Tagline = "all-in-one dev console · sql · cql · redis · loki · grafana · prometheus · jvm · btop · podman"

func BannerLines() []string {
	lines := make([]string, len(letters[0]))
	for _, l := range letters {
		for row := range lines {
			lines[row] += l[row]
		}
	}
	return lines
}

func BannerColor(col, width int, phase float64) tcell.Color {
	t := float64(col)/float64(max(1, width-1)) + phase
	t = t - math.Floor(t)
	if t < 0.5 {
		return theme.Lerp(theme.Cyan, theme.Magenta, t*2)
	}
	return theme.Lerp(theme.Magenta, theme.Cyan, (t-0.5)*2)
}

type Splash struct {
	*tview.Box
	started time.Time
	status  func() (string, string)
	onDone  func()
}

func NewSplash(status func() (string, string), onDone func()) *Splash {
	s := &Splash{Box: tview.NewBox(), started: time.Now(), status: status, onDone: onDone}
	s.SetBackgroundColor(theme.Color(theme.Bg))
	return s
}

func (s *Splash) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return s.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		s.onDone()
	})
}

func (s *Splash) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return s.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		if action == tview.MouseLeftClick {
			s.onDone()
		}
		return true, nil
	})
}

func (s *Splash) Draw(screen tcell.Screen) {
	sw, sh := screen.Size()
	s.SetRect(0, 0, sw, sh)
	bg := tcell.StyleDefault.Background(theme.Color(theme.Bg))
	fill(screen, 0, 0, sw, sh, bg)
	lines := BannerLines()
	width := runewidth.StringWidth(lines[0])
	phase := time.Since(s.started).Seconds() / 3
	y := max(0, sh/2-6)
	x := max(0, (sw-width)/2)
	for row, line := range lines {
		col := 0
		for _, r := range line {
			st := bg.Foreground(BannerColor(col, width, phase)).Bold(true)
			if r == '╗' || r == '╝' || r == '║' || r == '═' || r == '╔' || r == '╚' {
				st = bg.Foreground(theme.Lerp(theme.Border, theme.Purple, 0.5))
			}
			screen.SetContent(x+col, y+row, r, nil, st)
			col++
		}
	}
	center := func(row int, text string, st tcell.Style) {
		put(screen, max(0, (sw-runewidth.StringWidth(text))/2), row, sw, text, st)
	}
	center(y+7, "⚡ v"+Version+" · "+Tagline, bg.Foreground(theme.Color(theme.Dim)))
	color, text := s.status()
	spinner := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
	frame := string(spinner[int(time.Since(s.started).Milliseconds()/80)%len(spinner)])
	center(y+9, frame+" "+text, bg.Foreground(theme.Color(color)))
	center(y+11, "press any key", bg.Foreground(theme.Color(theme.Border)))
}
