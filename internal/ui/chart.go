package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/backend"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/mattn/go-runewidth"
)

const (
	chartHeight = 9
	chartLegend = 12
	chartWidth  = 100
)

var seriesColors = []string{theme.Cyan, theme.Magenta, theme.Lime, theme.Orange, theme.Purple, theme.Yellow, theme.Blue, theme.Red}

var brailleDots = [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

var bigDigits = map[rune][3]string{
	'0': {"┏━┓", "┃ ┃", "┗━┛"},
	'1': {" ┓ ", " ┃ ", "╺┻╸"},
	'2': {"╺━┓", "┏━┛", "┗━╸"},
	'3': {"╺━┓", " ━┫", "╺━┛"},
	'4': {"╻ ╻", "┗━┫", "  ╹"},
	'5': {"┏━╸", "┗━┓", "╺━┛"},
	'6': {"┏━╸", "┣━┓", "┗━┛"},
	'7': {"╺━┓", "  ┃", "  ╹"},
	'8': {"┏━┓", "┣━┫", "┗━┛"},
	'9': {"┏━┓", "┗━┫", "╺━┛"},
	'.': {" ", " ", "╹"},
	'-': {"   ", "╺━╸", "   "},
}

type dotCell struct {
	bits  rune
	color int
}

func seriesColor(i int) string {
	return seriesColors[i%len(seriesColors)]
}

func renderChart(b *strings.Builder, c *backend.Chart, width int) {
	if width <= 0 {
		width = chartWidth
	}
	width = max(width, 40)
	switch c.Kind {
	case "stat":
		renderStat(b, c, width)
	case "gauge":
		renderGauge(b, c, width)
	default:
		renderTimeseries(b, c, width)
	}
}

func stats(values []float64) (last, lo, hi float64) {
	last, lo, hi = math.NaN(), math.NaN(), math.NaN()
	for _, v := range values {
		if math.IsNaN(v) {
			continue
		}
		last = v
		if math.IsNaN(lo) || v < lo {
			lo = v
		}
		if math.IsNaN(hi) || v > hi {
			hi = v
		}
	}
	return last, lo, hi
}

func chartBounds(series []backend.Series) (lo, hi float64, t0, t1 int64) {
	lo, hi = math.NaN(), math.NaN()
	for _, s := range series {
		_, slo, shi := stats(s.Values)
		if !math.IsNaN(slo) && (math.IsNaN(lo) || slo < lo) {
			lo = slo
		}
		if !math.IsNaN(shi) && (math.IsNaN(hi) || shi > hi) {
			hi = shi
		}
		if len(s.Times) > 0 {
			if t0 == 0 || s.Times[0] < t0 {
				t0 = s.Times[0]
			}
			t1 = max(t1, s.Times[len(s.Times)-1])
		}
	}
	if lo >= 0 {
		lo = 0
	}
	if hi <= lo {
		hi = lo + 1
	}
	return lo, hi, t0, t1
}

func scaleTo(v, span float64, n int) int {
	if span <= 0 {
		return 0
	}
	return int(math.Round(v / span * float64(n-1)))
}

func drawLine(x0, y0, x1, y1 int, set func(x, y int)) {
	dx, dy := x1-x0, y1-y0
	sx, sy := 1, 1
	if dx < 0 {
		dx, sx = -dx, -1
	}
	if dy < 0 {
		dy, sy = -dy, -1
	}
	err := dx - dy
	for {
		set(x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func plot(series []backend.Series, w, h int, lo, hi float64, t0, t1 int64) [][]dotCell {
	grid := make([][]dotCell, h)
	for i := range grid {
		grid[i] = make([]dotCell, w)
	}
	dw, dh := w*2, h*4
	for si, s := range series {
		set := func(x, y int) {
			if x >= 0 && y >= 0 && x < dw && y < dh {
				cell := &grid[y/4][x/2]
				cell.bits |= brailleDots[y%4][x%2]
				cell.color = si
			}
		}
		px, py, prev, have := 0, 0, int64(0), false
		for i, v := range s.Values {
			if math.IsNaN(v) {
				have = false
				continue
			}
			x := scaleTo(float64(s.Times[i]-t0), float64(t1-t0), dw)
			y := dh - 1 - scaleTo(v-lo, hi-lo, dh)
			if have && (s.Step <= 0 || s.Times[i]-prev <= 2*s.Step) {
				drawLine(px, py, x, y, set)
			} else {
				set(x, y)
			}
			px, py, prev, have = x, y, s.Times[i], true
		}
	}
	return grid
}

func writeCells(b *strings.Builder, row []dotCell) {
	for i := 0; i < len(row); {
		j := i
		for j < len(row) && (row[j].bits == 0) == (row[i].bits == 0) && (row[j].bits == 0 || row[j].color == row[i].color) {
			j++
		}
		if row[i].bits == 0 {
			b.WriteString(strings.Repeat(" ", j-i))
		} else {
			var run strings.Builder
			for _, c := range row[i:j] {
				run.WriteRune(0x2800 | c.bits)
			}
			b.WriteString(colored(seriesColor(row[i].color), run.String()))
		}
		i = j
	}
}

func timeLabel(ms, span int64) string {
	layout := "15:04"
	if span > 24*time.Hour.Milliseconds() {
		layout = "01-02 15:04"
	}
	return time.UnixMilli(ms).Format(layout)
}

func timeAxis(t0, t1 int64, width int) string {
	line := []rune(strings.Repeat(" ", width))
	place := func(at int, text string) {
		for i, r := range text {
			if at+i >= 0 && at+i < width {
				line[at+i] = r
			}
		}
	}
	left, mid, right := timeLabel(t0, t1-t0), timeLabel(t0+(t1-t0)/2, t1-t0), timeLabel(t1, t1-t0)
	place(0, left)
	if width >= 3*len(mid)+4 {
		place(width/2-len(mid)/2, mid)
	}
	place(width-len(right), right)
	return string(line)
}

func renderTimeseries(b *strings.Builder, c *backend.Chart, width int) {
	lo, hi, t0, t1 := chartBounds(c.Series)
	if math.IsNaN(lo) || math.IsNaN(hi) {
		b.WriteString(colored(theme.Dim, "no data points in range") + "\n")
		return
	}
	mid := chartHeight / 2
	labels := map[int]string{
		0:               formatValue(hi, c.Unit),
		mid:             formatValue((hi+lo)/2, c.Unit),
		chartHeight - 1: formatValue(lo, c.Unit),
	}
	labelWidth := 0
	for _, l := range labels {
		labelWidth = max(labelWidth, len(l))
	}
	plotWidth := max(10, width-labelWidth-2)
	grid := plot(c.Series, plotWidth, chartHeight, lo, hi, t0, t1)
	for row := range chartHeight {
		label, tick := labels[row], "│"
		if label != "" {
			tick = "┤"
		}
		b.WriteString(colored(theme.Dim, fmt.Sprintf("%*s ", labelWidth, label)) + colored(theme.Border, tick))
		writeCells(b, grid[row])
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat(" ", labelWidth+1) + colored(theme.Border, "└"+strings.Repeat("─", plotWidth)) + "\n")
	b.WriteString(strings.Repeat(" ", labelWidth+2) + colored(theme.Dim, timeAxis(t0, t1, plotWidth)) + "\n")
	renderLegend(b, c, width)
}

func renderLegend(b *strings.Builder, c *backend.Chart, width int) {
	nameWidth := 0
	for _, s := range c.Series {
		nameWidth = max(nameWidth, runewidth.StringWidth(s.Name))
	}
	nameWidth = min(nameWidth, max(12, width/2))
	shown := c.Series[:min(len(c.Series), chartLegend)]
	cells := make([][3]string, len(shown))
	var widths [3]int
	for i, s := range shown {
		last, lo, hi := stats(s.Values)
		cells[i] = [3]string{formatValue(last, c.Unit), formatValue(lo, c.Unit), formatValue(hi, c.Unit)}
		for k, cell := range cells[i] {
			widths[k] = max(widths[k], len(cell))
		}
	}
	for i, s := range shown {
		name := truncate(s.Name, nameWidth)
		name += strings.Repeat(" ", nameWidth-runewidth.StringWidth(name))
		b.WriteString(colored(seriesColor(i), "● "+name))
		for k, label := range []string{"last", "min", "max"} {
			b.WriteString(colored(theme.Dim, "  "+label+" ") + colored(theme.Text, fmt.Sprintf("%-*s", widths[k], cells[i][k])))
		}
		b.WriteString("\n")
	}
	if len(c.Series) > len(shown) {
		b.WriteString(colored(theme.Dim, fmt.Sprintf("… %d more series", len(c.Series)-len(shown))) + "\n")
	}
}

func splitNumber(text string) (string, string) {
	i := 0
	for i < len(text) && strings.ContainsRune("-0123456789.", rune(text[i])) {
		i++
	}
	return text[:i], text[i:]
}

func bigText(digits string) [3]string {
	var rows [3]string
	for _, r := range digits {
		glyph := bigDigits[r]
		for i := range rows {
			rows[i] += glyph[i]
		}
	}
	return rows
}

func sparkline(values []float64, lo, hi float64, width int) string {
	const blocks = "▁▂▃▄▅▆▇█"
	levels := []rune(blocks)
	if len(values) == 0 || width <= 0 {
		return ""
	}
	width = min(width, len(values))
	var b strings.Builder
	for i := range width {
		from, to := i*len(values)/width, (i+1)*len(values)/width
		_, _, peak := stats(values[from:max(to, from+1)])
		if math.IsNaN(peak) {
			b.WriteRune(' ')
			continue
		}
		level := len(levels) / 2
		if hi > lo {
			level = int(math.Round((peak - lo) / (hi - lo) * float64(len(levels)-1)))
		}
		b.WriteRune(levels[level])
	}
	return b.String()
}

func renderStat(b *strings.Builder, c *backend.Chart, width int) {
	for i, s := range c.Series {
		if i == chartLegend {
			b.WriteString(colored(theme.Dim, fmt.Sprintf("… %d more series", len(c.Series)-i)) + "\n")
			return
		}
		last, lo, hi := stats(s.Values)
		digits, suffix := splitNumber(formatValue(last, c.Unit))
		rows := bigText(digits)
		color := seriesColor(i)
		for r, row := range rows {
			b.WriteString("  [" + color + "::b]" + row)
			if r == len(rows)-1 {
				b.WriteString(" " + suffix)
			}
			b.WriteString("[-::-]\n")
		}
		b.WriteString("  " + colored(theme.Dim, truncate(s.Name, width/2)) + "  " + colored(color, sparkline(s.Values, lo, hi, width-runewidth.StringWidth(s.Name)-6)) + "\n")
	}
}

func renderGauge(b *strings.Builder, c *backend.Chart, width int) {
	lasts := make([]float64, len(c.Series))
	nameWidth, valueWidth, highest := 0, 0, 0.0
	for i, s := range c.Series {
		lasts[i], _, _ = stats(s.Values)
		highest = max(highest, lasts[i])
		nameWidth = max(nameWidth, runewidth.StringWidth(s.Name))
		valueWidth = max(valueWidth, len(formatValue(lasts[i], c.Unit)))
	}
	limit := c.Max
	switch {
	case limit > 0:
	case c.Unit == "percent":
		limit = 100
	case c.Unit == "percentunit":
		limit = 1
	default:
		limit = highest
	}
	nameWidth = min(nameWidth, max(12, width/3))
	barWidth := max(10, width-nameWidth-valueWidth-4)
	for i, s := range c.Series {
		ratio := 0.0
		if limit > 0 {
			ratio = clamp01(lasts[i] / limit)
		}
		filled := int(math.Round(ratio * float64(barWidth)))
		name := truncate(s.Name, nameWidth)
		b.WriteString(colored(seriesColor(i), name+strings.Repeat(" ", nameWidth-runewidth.StringWidth(name))) + " ")
		for j := range barWidth {
			if j < filled {
				b.WriteString("[" + theme.Hex(theme.Heat(float64(j)/float64(max(1, barWidth-1)))) + "]■")
			} else {
				b.WriteString("[" + theme.Border + "]·")
			}
		}
		b.WriteString("[-] " + colored(theme.Text, formatValue(lasts[i], c.Unit)) + "\n")
	}
}

func compactNumber(v float64) string {
	suffix := ""
	for _, u := range []struct {
		size float64
		name string
	}{{1e9, "B"}, {1e6, "M"}, {1e3, "K"}} {
		if math.Abs(v) >= u.size {
			v, suffix = v/u.size, u.name
			break
		}
	}
	a := math.Abs(v)
	if a > 0 && a < 1 {
		return strconv.FormatFloat(v, 'g', 3, 64) + suffix
	}
	decimals := 0
	switch {
	case a < 10:
		decimals = 2
	case a < 100:
		decimals = 1
	}
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s + suffix
}

func formatValue(v float64, unit string) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	switch unit {
	case "bytes", "decbytes":
		if v < 0 {
			return "-" + bytesLabel(-v)
		}
		return bytesLabel(v)
	case "percent":
		return compactNumber(v) + "%"
	case "percentunit":
		return compactNumber(v*100) + "%"
	case "s":
		if v != 0 && math.Abs(v) < 1 {
			return compactNumber(v*1000) + "ms"
		}
		return compactNumber(v) + "s"
	case "ms":
		return compactNumber(v) + "ms"
	case "reqps":
		return compactNumber(v) + " req/s"
	}
	return compactNumber(v)
}
