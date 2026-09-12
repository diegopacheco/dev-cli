package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/sys"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const historySize = 400

type panel struct {
	*tview.Box
	draw func(screen tcell.Screen, x, y, w, h int)
}

func newPanel(title, color string, draw func(screen tcell.Screen, x, y, w, h int)) *panel {
	p := &panel{Box: tview.NewBox(), draw: draw}
	panelBox(p.Box, title, color)
	p.SetBorderStyle(tcell.StyleDefault.Foreground(theme.Color(color)).Background(theme.Color(theme.Bg)).Dim(true))
	return p
}

func (p *panel) Draw(screen tcell.Screen) {
	p.DrawForSubclass(screen, p)
	x, y, w, h := p.GetInnerRect()
	if w > 0 && h > 0 {
		p.draw(screen, x, y, w, h)
	}
}

type Dashboard struct {
	*tview.Flex
	queue   func(func())
	run     sys.Runner
	static  sys.Static
	current sys.Metrics
	prev    sys.Metrics
	samples int
	cpu     []float64
	netIn   []float64
	netOut  []float64
	read    []float64
	write   []float64
	volumes []sys.Volume
	procs   []sys.Process
	status  string
	started bool
}

func NewDashboard(run sys.Runner, queue func(func())) *Dashboard {
	d := &Dashboard{Flex: tview.NewFlex().SetDirection(tview.FlexRow), run: run, queue: queue}
	middle := tview.NewFlex().
		AddItem(newPanel(" ▦ mem ", theme.Magenta, d.drawMem), 0, 1, false).
		AddItem(newPanel(" ⛁ disks ", theme.Orange, d.drawDisks), 0, 1, false).
		AddItem(newPanel(" ⇅ net ", theme.Lime, d.drawNet), 0, 1, false)
	d.AddItem(newPanel(" ⚡ cpu ", theme.Cyan, d.drawCPU), 0, 2, false).
		AddItem(middle, 0, 3, false).
		AddItem(newPanel(" ☰ top processes ", theme.Purple, d.drawProcs), 0, 3, false)
	d.SetBackgroundColor(theme.Color(theme.Bg))
	return d
}

func (d *Dashboard) Title() string                { return "Dashboard" }
func (d *Dashboard) Root() tview.Primitive        { return d }
func (d *Dashboard) FocusTarget() tview.Primitive { return d }
func (d *Dashboard) Typing() bool                 { return false }
func (d *Dashboard) Hide()                        {}
func (d *Dashboard) Hints() string {
	return "live every second · Ctrl-N/P or F1-F11 switch tabs · q quit"
}

func (d *Dashboard) Show() {
	if d.started {
		return
	}
	d.started = true
	go func() {
		static := sys.LoadStatic(context.Background(), d.run)
		d.queue(func() { d.static = static })
	}()
	go d.sampleLoop()
	go d.slowLoop()
}

func (d *Dashboard) sampleLoop() {
	for {
		err := sys.StreamMetrics(context.Background(), d.run, func(m sys.Metrics) {
			d.queue(func() { d.Apply(m) })
		})
		d.queue(func() { d.status = "sampler restarting: " + err.Error() })
		time.Sleep(2 * time.Second)
	}
}

func (d *Dashboard) slowLoop() {
	for tick := 0; ; tick++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		procs, _ := sys.Processes(ctx, d.run)
		var volumes []sys.Volume
		if tick%5 == 0 {
			volumes = sys.Volumes(ctx, d.run)
		}
		cancel()
		d.queue(func() { d.ApplySlow(procs, volumes) })
		time.Sleep(2 * time.Second)
	}
}

func push(hist []float64, v float64) []float64 {
	hist = append(hist, v)
	if len(hist) > historySize {
		hist = hist[len(hist)-historySize:]
	}
	return hist
}

func (d *Dashboard) Apply(m sys.Metrics) {
	d.prev, d.current = d.current, m
	d.samples++
	d.status = ""
	d.cpu = push(d.cpu, m.CPU/100)
	if d.samples < 2 {
		return
	}
	elapsed := d.current.Time.Sub(d.prev.Time)
	d.netIn = push(d.netIn, sys.Rate(d.prev.NetIn, m.NetIn, elapsed))
	d.netOut = push(d.netOut, sys.Rate(d.prev.NetOut, m.NetOut, elapsed))
	d.read = push(d.read, sys.Rate(d.prev.DiskRead, m.DiskRead, elapsed))
	d.write = push(d.write, sys.Rate(d.prev.DiskWrite, m.DiskWrite, elapsed))
}

func (d *Dashboard) ApplySlow(procs []sys.Process, volumes []sys.Volume) {
	if procs != nil {
		sys.SortProcesses(procs, sys.ByCPU)
		d.procs = procs
	}
	if volumes != nil {
		d.volumes = volumes
	}
}

func last(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	return v[len(v)-1]
}

func peak(v []float64, floor float64) float64 {
	m := floor
	for _, x := range v {
		m = max(m, x)
	}
	return m
}

func scaled(v []float64, top float64) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / top
	}
	return out
}

func label(screen tcell.Screen, x, y, maxX int, parts ...string) int {
	for i := 0; i+1 < len(parts); i += 2 {
		x = put(screen, x, y, maxX, parts[i+1], style(parts[i]))
	}
	return x
}

func uptime(boot time.Time) string {
	if boot.IsZero() {
		return "?"
	}
	d := time.Since(boot)
	return fmt.Sprintf("%dd %dh %dm", int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60)
}

func (d *Dashboard) drawCPU(screen tcell.Screen, x, y, w, h int) {
	statsW := min(34, w/2)
	graphW := w - statsW - 1
	braille(screen, x, y, graphW, h, d.cpu, theme.Heat)
	pct := last(d.cpu)
	sx := x + graphW + 1
	maxX := x + w
	tcellPct := fmt.Sprintf("%5.1f%%", pct*100)
	label(screen, sx, y, maxX, theme.Cyan, "CPU ", theme.Hex(theme.Heat(pct)), tcellPct)
	meter(screen, sx+11, y, maxX-sx-11, pct)
	m := d.current
	rows := [][]string{
		{theme.Dim, "user ", theme.Lime, fmt.Sprintf("%.1f%%", m.User), theme.Dim, "   sys ", theme.Orange, fmt.Sprintf("%.1f%%", m.System)},
		{theme.Dim, "model ", theme.Text, d.static.Model},
		{theme.Dim, "cores ", theme.Magenta, fmt.Sprint(d.static.Cores), theme.Dim, "   up ", theme.Text, uptime(d.static.BootTime)},
		{theme.Dim, "load ", theme.Yellow, m.Load},
		{theme.Dim, "procs ", theme.Purple, fmt.Sprint(m.Processes), theme.Dim, "   threads ", theme.Purple, fmt.Sprint(m.Threads)},
	}
	for i, r := range rows {
		if y+2+i >= y+h {
			break
		}
		label(screen, sx, y+2+i, maxX, r...)
	}
	if d.status != "" {
		put(screen, sx, y+h-1, maxX, d.status, style(theme.Red))
	}
	if d.samples == 0 {
		put(screen, x+2, y+h/2, x+graphW, "sampling cpu…", style(theme.Dim))
	}
}

func (d *Dashboard) drawMem(screen tcell.Screen, x, y, w, h int) {
	m := d.current
	total := float64(d.static.MemTotal)
	if total == 0 {
		total = float64(m.MemUsed + m.MemFree)
	}
	label(screen, x, y, x+w, theme.Dim, "total ", theme.Text, bytesLabel(total))
	rows := []struct {
		name  string
		value float64
		of    float64
		color string
	}{
		{"used", float64(m.MemUsed), total, theme.Magenta},
		{"wired", float64(m.MemWired), total, theme.Orange},
		{"compressed", float64(m.MemCompressed), total, theme.Yellow},
		{"free", float64(m.MemFree), total, theme.Lime},
		{"swap", float64(d.static.SwapUsed), float64(d.static.SwapTotal), theme.Red},
	}
	for i, r := range rows {
		ry := y + 1 + i*2
		if ry+1 >= y+h {
			break
		}
		ratio := 0.0
		if r.of > 0 {
			ratio = r.value / r.of
		}
		label(screen, x, ry, x+w, r.color, fmt.Sprintf("%-11s", r.name), theme.Text, fmt.Sprintf("%7s", bytesLabel(r.value)), theme.Dim, fmt.Sprintf(" %3.0f%%", ratio*100))
		meter(screen, x, ry+1, w, ratio)
	}
}

func volumeName(mount string) string {
	if mount == "/System/Volumes/Data" {
		return "Data"
	}
	if mount == "/" {
		return "root"
	}
	return filepath.Base(mount)
}

func (d *Dashboard) drawDisks(screen tcell.Screen, x, y, w, h int) {
	row := y
	for _, v := range d.volumes {
		if row+1 >= y+h-4 {
			break
		}
		ratio := float64(v.Used) / float64(max(1, v.Total))
		label(screen, x, row, x+w, theme.Orange, fmt.Sprintf("%-10s", volumeName(v.Mount)), theme.Text, bytesLabel(float64(v.Used))+"/"+bytesLabel(float64(v.Total)), theme.Hex(theme.Heat(ratio)), fmt.Sprintf(" %3.0f%%", ratio*100))
		meter(screen, x, row+1, w, ratio)
		row += 2
	}
	graphH := max(1, (y+h-row-2)/2)
	top := peak(append(append([]float64{}, d.read...), d.write...), 1<<20)
	label(screen, x, row, x+w, theme.Dim, "read  ", theme.Yellow, bytesLabel(last(d.read))+"/s")
	braille(screen, x, row+1, w, graphH, scaled(d.read, top), func(l float64) tcell.Color { return theme.Lerp(theme.Orange, theme.Yellow, l) })
	row += graphH + 1
	if row < y+h {
		label(screen, x, row, x+w, theme.Dim, "write ", theme.Red, bytesLabel(last(d.write))+"/s")
		braille(screen, x, row+1, w, min(graphH, y+h-row-1), scaled(d.write, top), func(l float64) tcell.Color { return theme.Lerp(theme.Red, theme.Magenta, l) })
	}
}

func (d *Dashboard) drawNet(screen tcell.Screen, x, y, w, h int) {
	graphH := max(1, (h-2)/2)
	top := peak(append(append([]float64{}, d.netIn...), d.netOut...), 64<<10)
	label(screen, x, y, x+w, theme.Cyan, "▼ in  ", theme.Text, fmt.Sprintf("%-10s", bytesLabel(last(d.netIn))+"/s"), theme.Dim, " total ", theme.Text, bytesLabel(float64(d.current.NetIn)))
	braille(screen, x, y+1, w, graphH, scaled(d.netIn, top), func(l float64) tcell.Color { return theme.Lerp(theme.Blue, theme.Cyan, l) })
	oy := y + 1 + graphH
	label(screen, x, oy, x+w, theme.Magenta, "▲ out ", theme.Text, fmt.Sprintf("%-10s", bytesLabel(last(d.netOut))+"/s"), theme.Dim, " total ", theme.Text, bytesLabel(float64(d.current.NetOut)))
	braille(screen, x, oy+1, w, min(graphH, y+h-oy-1), scaled(d.netOut, top), func(l float64) tcell.Color { return theme.Lerp(theme.Purple, theme.Magenta, l) })
	put(screen, x+w-12, y, x+w, fmt.Sprintf("peak %s", bytesLabel(top)), style(theme.Dim))
}

func (d *Dashboard) drawProcs(screen tcell.Screen, x, y, w, h int) {
	cmdW := max(10, w-54)
	header := fmt.Sprintf("%7s  %-12s %6s  %-12s %8s  %s", "PID", "USER", "CPU%", "", "MEM", "COMMAND")
	put(screen, x, y, x+w, header, style(theme.Cyan).Bold(true))
	for i, p := range d.procs {
		row := y + 1 + i
		if row >= y+h {
			break
		}
		heat := p.CPU / 100 / float64(max(1, d.static.Cores)) * 4
		cx := label(screen, x, row, x+w, theme.Dim, fmt.Sprintf("%7d  ", p.PID), theme.Purple, fmt.Sprintf("%-12s ", truncate(p.User, 12)), theme.Hex(theme.Heat(heat)), fmt.Sprintf("%6.1f  ", p.CPU))
		meter(screen, cx, row, 12, heat)
		label(screen, cx+13, row, x+w, theme.Magenta, fmt.Sprintf("%8s  ", bytesLabel(float64(p.RSS))), theme.Text, truncate(strings.TrimSpace(p.Name()), cmdW))
	}
}
