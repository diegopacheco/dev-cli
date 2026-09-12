package ui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/diegopacheco/dev-cli/internal/backend"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Console struct {
	*tview.Flex
	title      string
	queue      func(func())
	backend    backend.Backend
	target     string
	connected  bool
	connecting bool
	running    bool
	asJSON     bool
	results    []backend.Result
	err        error
	cancel     context.CancelFunc
	mu         sync.Mutex
	backendMu  sync.Mutex
	gen        atomic.Int64
	conn       *tview.TextView
	connEdit   *tview.InputField
	connPages  *tview.Pages
	editor     *Editor
	output     *tview.TextView
	info       *tview.TextView
	body       *responsive
	focus      func(tview.Primitive)
}

type responsive struct {
	*tview.Flex
	threshold int
}

func (r *responsive) Draw(screen tcell.Screen) {
	_, _, w, _ := r.GetRect()
	if w >= r.threshold {
		r.SetDirection(tview.FlexColumn)
	} else {
		r.SetDirection(tview.FlexRow)
	}
	r.Flex.Draw(screen)
}

func panelBox(box *tview.Box, title, color string) {
	box.SetBorder(true).
		SetBorderStyle(tcell.StyleDefault.Foreground(theme.Color(theme.Border)).Background(theme.Color(theme.Bg))).
		SetTitle(title).
		SetTitleColor(theme.Color(color)).
		SetTitleAlign(tview.AlignLeft).
		SetBackgroundColor(theme.Color(theme.Bg))
}

func NewConsole(title string, b backend.Backend, queue func(func()), focus func(tview.Primitive)) *Console {
	c := &Console{
		Flex:    tview.NewFlex().SetDirection(tview.FlexRow),
		title:   title,
		queue:   queue,
		backend: b,
		target:  b.DefaultTarget(),
		focus:   focus,
	}
	c.conn = tview.NewTextView().SetDynamicColors(true)
	c.conn.SetBackgroundColor(theme.Color(theme.Bg))
	c.connEdit = tview.NewInputField().
		SetLabel(" ⛁ " + title + " ▸ ").
		SetLabelColor(theme.Color(theme.Magenta)).
		SetFieldBackgroundColor(theme.Color("#1b2340")).
		SetFieldTextColor(theme.Color(theme.Text))
	c.connEdit.SetBackgroundColor(theme.Color(theme.Bg))
	c.connEdit.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			c.target = c.connEdit.GetText()
			c.Connect()
		}
		c.connPages.SwitchToPage("view")
		c.focus(c.editor)
	})
	c.connPages = tview.NewPages().AddPage("view", c.conn, true, true).AddPage("edit", c.connEdit, true, false)

	c.editor = NewEditor(b.Language())
	panelBox(c.editor.Box, " ✎ editor ", theme.Cyan)
	c.editor.runsOnEnter = b.RunsOnEnter
	c.editor.onRun = c.Run
	c.editor.SetInputCapture(c.capture)

	c.output = tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	panelBox(c.output.Box, " ◆ results · table ", theme.Lime)
	c.output.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyTab {
			c.focus(c.editor)
			return nil
		}
		return c.capture(event)
	})

	c.info = tview.NewTextView().SetDynamicColors(true)
	c.info.SetBackgroundColor(theme.Color(theme.Bg))

	c.body = &responsive{Flex: tview.NewFlex(), threshold: 120}
	c.body.AddItem(c.editor, 0, 2, true).AddItem(c.output, 0, 3, false)

	c.AddItem(c.connPages, 1, 0, false).AddItem(c.body, 0, 1, true).AddItem(c.info, 1, 0, false)
	c.SetBackgroundColor(theme.Color(theme.Bg))
	c.renderConn()
	c.setInfo(theme.Dim, "not connected")
	return c
}

func (c *Console) Title() string                { return c.title }
func (c *Console) Root() tview.Primitive        { return c }
func (c *Console) FocusTarget() tview.Primitive { return c.editor }
func (c *Console) Hide()                        {}
func (c *Console) Typing() bool                 { return true }

func (c *Console) Hints() string {
	return "Ctrl-R run · Enter run/newline · Tab complete · ↑↓ history · Ctrl-T table/json · Ctrl-E connection · F5 words · Ctrl-L clear · Esc cancel"
}

func (c *Console) Show() {
	if !c.connected && !c.connecting {
		c.Connect()
	}
}

func (c *Console) capture(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyCtrlT:
		c.ToggleJSON()
		return nil
	case tcell.KeyCtrlE:
		c.connEdit.SetText(c.target)
		c.connPages.SwitchToPage("edit")
		c.focus(c.connEdit)
		return nil
	case tcell.KeyF5:
		c.loadWords()
		return nil
	case tcell.KeyPgUp:
		row, col := c.output.GetScrollOffset()
		c.output.ScrollTo(max(0, row-10), col)
		return nil
	case tcell.KeyPgDn:
		row, col := c.output.GetScrollOffset()
		c.output.ScrollTo(row+10, col)
		return nil
	case tcell.KeyEscape:
		c.mu.Lock()
		cancel := c.cancel
		c.mu.Unlock()
		if c.running && cancel != nil {
			cancel()
			return nil
		}
	}
	return event
}

func (c *Console) renderConn() {
	state := colored(theme.Dim, "○ idle")
	switch {
	case c.connecting:
		state = colored(theme.Yellow, "◌ connecting")
	case c.connected:
		state = colored(theme.Lime, "● connected")
	case c.err != nil:
		state = colored(theme.Red, "✖ offline")
	}
	c.conn.SetText(colored(theme.Magenta, " ⛁ "+c.title+" ▸ ") + colored(theme.Text, MaskTarget(c.target)) + "  " + state)
}

func (c *Console) setInfo(color, text string) {
	c.info.SetText(" " + colored(color, text))
}

func (c *Console) Target() string  { return c.target }
func (c *Console) Connected() bool { return c.connected }
func (c *Console) Editor() *Editor { return c.editor }
func (c *Console) ToggleJSON()     { c.asJSON = !c.asJSON; c.render() }
func (c *Console) ReloadWords()    { c.loadWords() }
func (c *Console) SetTarget(t string) {
	c.target = t
	c.Connect()
}

func (c *Console) Connect() {
	c.connecting = true
	c.connected = false
	c.renderConn()
	c.setInfo(theme.Yellow, "connecting to "+MaskTarget(c.target))
	target := c.target
	gen := c.gen.Add(1)
	go func() {
		c.backendMu.Lock()
		defer c.backendMu.Unlock()
		if c.gen.Load() != gen {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := c.backend.Connect(ctx, target)
		var words []string
		if err == nil {
			words = c.backend.Words(ctx)
		}
		c.queue(func() {
			if c.gen.Load() == gen {
				c.connectDone(err, words)
			}
		})
	}()
}

func (c *Console) connectDone(err error, words []string) {
	c.connecting = false
	c.connected = err == nil
	c.err = err
	if err != nil {
		c.setInfo(theme.Red, "✖ "+err.Error()+"  (Ctrl-E to edit the connection)")
	} else {
		c.editor.SetWords(words)
		c.setInfo(theme.Lime, fmt.Sprintf("● connected · %d completion words loaded", len(words)))
	}
	c.renderConn()
}

func (c *Console) loadWords() {
	if !c.connected {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c.backendMu.Lock()
		words := c.backend.Words(ctx)
		c.backendMu.Unlock()
		c.queue(func() {
			c.editor.SetWords(words)
			c.setInfo(theme.Lime, fmt.Sprintf("● %d completion words reloaded", len(words)))
		})
	}()
}

func (c *Console) Run(text string) {
	if c.running {
		return
	}
	if !c.connected {
		c.setInfo(theme.Red, "✖ not connected · Ctrl-E to edit the connection and press Enter")
		return
	}
	c.running = true
	c.setInfo(theme.Yellow, "◌ running… (Esc cancels)")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	go func() {
		defer cancel()
		start := time.Now()
		c.backendMu.Lock()
		results, err := c.backend.Execute(ctx, text)
		c.backendMu.Unlock()
		elapsed := time.Since(start)
		c.queue(func() { c.runDone(results, err, elapsed) })
	}()
}

func (c *Console) runDone(results []backend.Result, err error, elapsed time.Duration) {
	c.running = false
	c.results, c.err = results, err
	c.render()
	c.output.ScrollToBeginning()
	if err != nil {
		c.setInfo(theme.Red, "✖ "+err.Error())
		return
	}
	c.setInfo(theme.Lime, fmt.Sprintf("✔ %d results in %s", len(results), Duration(elapsed)))
}

func (c *Console) render() {
	mode := "table"
	if c.asJSON {
		mode = "json"
	}
	c.output.SetTitle(" ◆ results · " + mode + " ")
	c.output.SetText(RenderResults(c.results, c.err, c.asJSON))
}
