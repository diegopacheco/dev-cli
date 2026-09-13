package ui

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/discover"
	"github.com/diegopacheco/dev-cli/internal/sys"
	"github.com/gdamore/tcell/v2"
)

const (
	captureWidth  = 170
	captureHeight = 50
)

type captureStep struct {
	query string
	json  bool
	after string
}

var captureQueries = map[string]captureStep{
	"MySQL":      {query: "SELECT u.id, u.name, u.profile, COUNT(o.id) AS orders, SUM(o.total) AS spent\nFROM users u\nLEFT JOIN orders o ON o.user_id = u.id\nGROUP BY u.id\nORDER BY spent DESC;"},
	"Postgres":   {query: "SELECT name, profile->'tags' AS tags, profile\nFROM users\nWHERE (profile->>'level')::int > 6\nORDER BY name;", json: true},
	"SQLite":     {query: "SELECT h.name, h.region, m.name AS metric, m.value, m.labels\nFROM hosts h\nJOIN metrics m ON m.host_id = h.id;", after: "SELECT name, value\nFROM met"},
	"Cassandra":  {query: "SELECT id, name, email, tags, profile\nFROM devcli.users;"},
	"Redis":      {query: "GET user:1\nHGETALL session:9f2c\nZRANGE leaderboard 0 -1 WITHSCORES"},
	"Loki":       {query: `{env="dev"} != "cache hit"`},
	"Grafana":    {query: "chart devcli-metrics"},
	"Prometheus": {query: "all"},
}

func Capture(targets Targets, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	updates := make(chan func(), 4096)
	a := New(targets, func(f func()) { updates <- f })
	settle := func(timeout time.Duration, done func() bool) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if done() {
				return true
			}
			select {
			case f := <-updates:
				f()
			case <-time.After(50 * time.Millisecond):
			}
		}
		return done()
	}
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	screen.SetSize(captureWidth, captureHeight)

	a.Dashboard.Show()
	found, err := discover.Containers(context.Background(), sys.Run)
	if err != nil {
		return err
	}
	shots := []struct {
		name    string
		prepare func() error
	}{
		{"splash", func() error {
			a.SetFound(found)
			a.ShowSplash()
			return nil
		}},
		{"dashboard", func() error {
			a.Switch(0)
			if !settle(45*time.Second, func() bool {
				return a.Dashboard.samples >= 30 && a.Dashboard.procs != nil && a.Dashboard.volumes != nil && a.Dashboard.static.Cores > 0
			}) {
				return fmt.Errorf("dashboard did not collect metrics")
			}
			return nil
		}},
		{"processes", func() error {
			a.Switch(1)
			if !settle(15*time.Second, func() bool { return len(a.Processes.all) > 0 }) {
				return fmt.Errorf("process list stayed empty")
			}
			return nil
		}},
		{"containers", func() error {
			a.Switch(2)
			if !settle(20*time.Second, func() bool { return a.Containers.list != nil }) {
				return fmt.Errorf("container list did not load")
			}
			return nil
		}},
		{"threads", func() error {
			a.Switch(3)
			if !settle(15*time.Second, func() bool { return len(a.Threads.jvms) > 0 }) {
				return fmt.Errorf("no JVM found; start one with scripts/start-all.sh")
			}
			for _, p := range a.Threads.jvms {
				if strings.Contains(p.Main, "DevcliJvm") {
					a.Threads.SelectPID(p.PID)
				}
			}
			a.Threads.DumpSelected()
			if !settle(20*time.Second, func() bool { return a.Threads.dumpPID != 0 }) {
				return fmt.Errorf("thread dump failed")
			}
			return nil
		}},
	}
	for i, c := range a.Consoles {
		c, index := c, i+4
		step := captureQueries[c.Title()]
		shots = append(shots, struct {
			name    string
			prepare func() error
		}{strings.ToLower(c.Title()), func() error {
			a.Switch(index)
			settle(15*time.Second, func() bool { return !c.connecting })
			if !c.connected {
				return fmt.Errorf("%s: %v", c.Title(), c.err)
			}
			c.editor.SetText(step.query)
			c.editor.run()
			settle(30*time.Second, func() bool { return !c.running })
			if c.err != nil {
				return fmt.Errorf("%s: %v", c.Title(), c.err)
			}
			c.asJSON = step.json
			c.render()
			if step.after != "" {
				c.editor.SetText(step.after)
				c.editor.refreshPopup()
			}
			return nil
		}})
	}
	overlays := []struct {
		name    string
		prepare func() error
	}{
		{"connect", func() error {
			a.Switch(a.TabIndex("dashboard"))
			a.SetFound(found)
			a.promptFound()
			return nil
		}},
		{"palette", func() error {
			a.Switch(a.TabIndex("postgres"))
			a.SetFound(found)
			a.OpenPalette()
			a.Palette.SetQuery("post")
			return nil
		}},
		{"shortcuts", func() error {
			a.OpenShortcuts()
			return nil
		}},
	}
	shots = append(shots, overlays...)
	for i, shot := range shots {
		a.splashOn.Store(false)
		for _, page := range []string{"splash", "connect", "palette", "shortcuts"} {
			a.root.RemovePage(page)
		}
		fmt.Fprintln(os.Stderr, "capturing", shot.name)
		if err := shot.prepare(); err != nil {
			return err
		}
		settle(300*time.Millisecond, func() bool { return false })
		a.root.SetRect(0, 0, captureWidth, captureHeight)
		screen.Clear()
		a.root.Draw(screen)
		screen.Show()
		path := filepath.Join(dir, fmt.Sprintf("%02d-%s.html", i+1, shot.name))
		if err := os.WriteFile(path, []byte(screenHTML(screen, shot.name)), 0644); err != nil {
			return err
		}
	}
	return nil
}

func screenHTML(screen tcell.Screen, name string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>devcli " + name + "</title><style>body{margin:0;padding:18px;background:#0b0f1a}pre{font:14px/19px 'JetBrains Mono','SF Mono',Menlo,monospace;margin:0;width:max-content;border-radius:10px;overflow:hidden;box-shadow:0 20px 60px #00e5ff22}span{white-space:pre;display:inline-block;height:19px;vertical-align:top}</style></head><body><pre>")
	for y := 0; y < captureHeight; y++ {
		for x := 0; x < captureWidth; x++ {
			r, comb, st, width := screen.GetContent(x, y)
			if width <= 0 {
				width = 1
			}
			if r == 0 {
				r = ' '
			}
			fg, bg, attr := st.Decompose()
			fr, fgc, fb := fg.RGB()
			br, bgc, bb := bg.RGB()
			if fr < 0 {
				fr, fgc, fb = 215, 222, 240
			}
			if br < 0 {
				br, bgc, bb = 11, 15, 26
			}
			if attr&tcell.AttrDim != 0 {
				fr, fgc, fb = fr*6/10, fgc*6/10, fb*6/10
			}
			if r == '█' {
				r, br, bgc, bb = ' ', fr, fgc, fb
			}
			weight := "normal"
			if attr&tcell.AttrBold != 0 {
				weight = "bold"
			}
			fmt.Fprintf(&b, "<span style=\"color:rgb(%d,%d,%d);background:rgb(%d,%d,%d);font-weight:%s;width:%dch\">%s</span>", fr, fgc, fb, br, bgc, bb, weight, width, html.EscapeString(string(append([]rune{r}, comb...))))
			x += width - 1
		}
		b.WriteByte('\n')
	}
	b.WriteString("</pre></body></html>")
	return b.String()
}
