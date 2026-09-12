package ui

import (
	"fmt"
	"math"

	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

func style(fg string) tcell.Style {
	return tcell.StyleDefault.Foreground(theme.Color(fg)).Background(theme.Color(theme.Bg))
}

func put(screen tcell.Screen, x, y, maxX int, text string, st tcell.Style) int {
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			w = 1
		}
		if x+w > maxX {
			break
		}
		screen.SetContent(x, y, r, nil, st)
		x += w
	}
	return x
}

func fill(screen tcell.Screen, x, y, w, h int, st tcell.Style) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			screen.SetContent(col, row, ' ', nil, st)
		}
	}
}

func meter(screen tcell.Screen, x, y, w int, ratio float64) {
	if w <= 0 {
		return
	}
	filled := int(math.Round(clamp01(ratio) * float64(w)))
	for i := 0; i < w; i++ {
		st := style(theme.Border)
		if i < filled {
			st = tcell.StyleDefault.Foreground(theme.Heat(float64(i) / float64(max(1, w-1)))).Background(theme.Color(theme.Bg))
		}
		screen.SetContent(x+i, y, '■', nil, st)
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}

var brailleLeft = [4]rune{0x40, 0x04, 0x02, 0x01}
var brailleRight = [4]rune{0x80, 0x20, 0x10, 0x08}

func dotsInCell(v float64, cellFromBottom, h int) int {
	total := int(math.Round(clamp01(v) * float64(h*4)))
	return max(0, min(4, total-cellFromBottom*4))
}

func braille(screen tcell.Screen, x, y, w, h int, values []float64, color func(level float64) tcell.Color) {
	if w <= 0 || h <= 0 {
		return
	}
	if len(values) > w {
		values = values[len(values)-w:]
	}
	offset := w - len(values)
	sample := func(i int) float64 {
		if i < 0 || i < offset {
			return -1
		}
		return values[i-offset]
	}
	for col := 0; col < w; col++ {
		left, right := sample(col), sample(col)
		if prev := sample(col - 1); prev >= 0 && left >= 0 {
			left = (prev + left) / 2
		}
		for row := 0; row < h; row++ {
			fromBottom := h - 1 - row
			cell := rune(0x2800)
			if left >= 0 {
				for d := 0; d < dotsInCell(left, fromBottom, h); d++ {
					cell |= brailleLeft[d]
				}
			}
			if right >= 0 {
				for d := 0; d < dotsInCell(right, fromBottom, h); d++ {
					cell |= brailleRight[d]
				}
			}
			level := float64(fromBottom) / float64(max(1, h-1))
			st := tcell.StyleDefault.Background(theme.Color(theme.Bg)).Foreground(color(level))
			if cell == 0x2800 {
				cell = ' '
			}
			screen.SetContent(x+col, y+row, cell, nil, st)
		}
	}
}

func bytesLabel(n float64) string {
	units := []string{"B", "K", "M", "G", "T", "P"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f%s", n, units[i])
	}
	if n >= 100 {
		return fmt.Sprintf("%.0f%s", n, units[i])
	}
	return fmt.Sprintf("%.1f%s", n, units[i])
}
