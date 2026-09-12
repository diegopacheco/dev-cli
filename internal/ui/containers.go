package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/diegopacheco/dev-cli/internal/sys"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Containers struct {
	*tview.Flex
	run     sys.Runner
	queue   func(func())
	confirm confirmFunc
	suspend func(func()) bool
	runtime string
	table   *tview.Table
	info    *tview.TextView
	list    []sys.Container
	visible atomic.Bool
	loop    bool
}

func NewContainers(run sys.Runner, queue func(func()), confirm confirmFunc, suspend func(func()) bool) *Containers {
	c := &Containers{Flex: tview.NewFlex().SetDirection(tview.FlexRow), run: run, queue: queue, confirm: confirm, suspend: suspend}
	c.runtime, _ = sys.Runtime()
	c.table = newTable(" ⬢ containers ", theme.Blue)
	c.table.SetInputCapture(c.keys)
	c.info = newInfo()
	c.AddItem(c.table, 0, 1, true).AddItem(c.info, 1, 0, false)
	c.SetBackgroundColor(theme.Color(theme.Bg))
	return c
}

func (c *Containers) Title() string                { return "Containers" }
func (c *Containers) Root() tview.Primitive        { return c }
func (c *Containers) FocusTarget() tview.Primitive { return c.table }
func (c *Containers) Typing() bool                 { return false }
func (c *Containers) Hide()                        { c.visible.Store(false) }
func (c *Containers) Hints() string {
	return "Enter/e shell · s stop · S start · k kill · d remove · r refresh"
}

func (c *Containers) Show() {
	c.visible.Store(true)
	if c.loop {
		return
	}
	c.loop = true
	go func() {
		for {
			if c.visible.Load() {
				c.Refresh()
			}
			time.Sleep(3 * time.Second)
		}
	}()
}

func (c *Containers) setInfo(color, text string) {
	c.info.SetText(" " + colored(color, text))
}

func (c *Containers) Refresh() {
	if c.runtime == "" {
		c.queue(func() { c.setInfo(theme.Red, "✖ neither podman nor docker found on PATH") })
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	list, err := sys.Containers(ctx, c.run, c.runtime)
	c.queue(func() {
		if err != nil {
			c.setInfo(theme.Red, "✖ "+err.Error())
			return
		}
		sort.SliceStable(list, func(i, j int) bool {
			return strings.EqualFold(list[i].State, "running") && !strings.EqualFold(list[j].State, "running")
		})
		c.list = list
		c.render()
	})
}

func stateColor(state string) string {
	switch strings.ToLower(state) {
	case "running":
		return theme.Lime
	case "exited", "stopped", "created":
		return theme.Dim
	case "paused":
		return theme.Yellow
	}
	return theme.Red
}

func stateIcon(state string) string {
	switch strings.ToLower(state) {
	case "running":
		return "● "
	case "paused":
		return "◐ "
	}
	return "○ "
}

func (c *Containers) render() {
	row, _ := c.table.GetSelection()
	c.table.Clear()
	headerRow(c.table, "STATE", "NAME", "ID", "IMAGE", "PORTS", "STATUS")
	running := 0
	for i, ct := range c.list {
		r := i + 1
		color := stateColor(ct.State)
		if strings.EqualFold(ct.State, "running") {
			running++
		}
		c.table.SetCell(r, 0, cell(stateIcon(ct.State)+ct.State, color))
		c.table.SetCell(r, 1, cell(ct.Name, theme.Magenta))
		c.table.SetCell(r, 2, cell(ct.ID, theme.Dim))
		c.table.SetCell(r, 3, cell(ct.Image, theme.Cyan))
		c.table.SetCell(r, 4, cell(ct.Ports, theme.Orange))
		c.table.SetCell(r, 5, cell(ct.Status, color).SetExpansion(1))
	}
	if len(c.list) > 0 {
		c.table.Select(max(1, min(row, len(c.list))), 0)
	}
	c.setInfo(theme.Lime, fmt.Sprintf("%d containers · %d running · runtime %s", len(c.list), running, filepath.Base(c.runtime)))
}

func (c *Containers) selected() (sys.Container, bool) {
	row, _ := c.table.GetSelection()
	if row < 1 || row > len(c.list) {
		return sys.Container{}, false
	}
	return c.list[row-1], true
}

func (c *Containers) keys(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyEnter || event.Rune() == 'e':
		c.shell()
	case event.Rune() == 's':
		c.act(sys.Stop, false)
	case event.Rune() == 'S':
		c.act(sys.Start, false)
	case event.Rune() == 'k':
		c.act(sys.Kill, true)
	case event.Rune() == 'd':
		c.act(sys.Remove, true)
	case event.Rune() == 'r':
		go c.Refresh()
	default:
		return event
	}
	return nil
}

func (c *Containers) act(action sys.Action, confirm bool) {
	ct, ok := c.selected()
	if !ok {
		return
	}
	do := func() {
		c.setInfo(theme.Yellow, fmt.Sprintf("◌ %s %s…", action, ct.Name))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := sys.ContainerAction(ctx, c.run, c.runtime, action, ct.ID)
			c.queue(func() {
				if err != nil {
					c.setInfo(theme.Red, "✖ "+err.Error())
					return
				}
				c.setInfo(theme.Lime, fmt.Sprintf("✔ %s %s", action, ct.Name))
			})
			c.Refresh()
		}()
	}
	if !confirm {
		do()
		return
	}
	c.confirm(string(action)+" container", fmt.Sprintf("%s container %s?\n\n%s\n%s", strings.ToUpper(string(action)), ct.Name, ct.Image, ct.ID), string(action), do)
}

func (c *Containers) shell() {
	ct, ok := c.selected()
	if !ok {
		return
	}
	if !strings.EqualFold(ct.State, "running") {
		c.setInfo(theme.Red, "✖ "+ct.Name+" is not running · press S to start it")
		return
	}
	var runErr error
	c.suspend(func() {
		cmd := sys.ShellCommand(c.runtime, ct.ID)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		fmt.Printf("\n\033[1;35m⬢ devcli\033[0m shell into \033[36m%s\033[0m, exit to return\n\n", ct.Name)
		runErr = cmd.Run()
	})
	if runErr != nil {
		c.setInfo(theme.Red, "✖ shell: "+runErr.Error())
		return
	}
	c.setInfo(theme.Lime, "✔ back from "+ct.Name)
}

func (c *Containers) SelectID(id string) bool {
	for i, ct := range c.list {
		if ct.ID == id {
			c.table.Select(i+1, 0)
			return true
		}
	}
	return false
}
