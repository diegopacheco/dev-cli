package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/backend"
	"github.com/diegopacheco/dev-cli/internal/discover"
	"github.com/diegopacheco/dev-cli/internal/jvm"
	"github.com/diegopacheco/dev-cli/internal/sys"
	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/diegopacheco/dev-cli/internal/ui"
	"github.com/rivo/tview"
)

type Options struct {
	Mode       string
	Dialect    string
	Query      string
	Target     string
	Tab        string
	Capture    string
	JSON       bool
	NoColor    bool
	Quiet      bool
	Version    bool
	Help       bool
	NoDiscover bool
	Timeout    time.Duration
}

type IO struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	InTTY  bool
	OutTTY bool
	ErrTTY bool
	Width  int
}

var modeFlags = []string{"sql", "cassandra", "redis", "loki", "grafana", "prometheus", "ps", "containers", "threads", "discover"}

var needsQuery = map[string]bool{"sql": true, "cassandra": true, "redis": true, "loki": true, "grafana": true, "prometheus": true}

func Parse(args []string) (Options, error) {
	var o Options
	fs := flag.NewFlagSet("devcli", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	modes := map[string]*bool{}
	for _, m := range modeFlags {
		modes[m] = fs.Bool(m, false, "")
	}
	cql := fs.Bool("cql", false, "")
	mysql := fs.Bool("mysql", false, "")
	postgres := fs.Bool("postgres", false, "")
	sqlite := fs.Bool("sqlite", false, "")
	sqllite := fs.Bool("sqllite", false, "")
	fs.BoolVar(&o.JSON, "json", false, "")
	fs.StringVar(&o.Target, "target", "", "")
	fs.StringVar(&o.Tab, "tab", "dashboard", "")
	fs.StringVar(&o.Capture, "capture", "", "")
	fs.BoolVar(&o.NoColor, "no-color", false, "")
	fs.BoolVar(&o.Quiet, "q", false, "")
	fs.BoolVar(&o.Quiet, "quiet", false, "")
	fs.BoolVar(&o.Version, "version", false, "")
	fs.BoolVar(&o.NoDiscover, "no-discover", false, "")
	fs.DurationVar(&o.Timeout, "timeout", 30*time.Second, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			o.Help = true
			return o, nil
		}
		return o, err
	}
	if *cql {
		*modes["cassandra"] = true
	}
	var dialects []string
	for name, on := range map[string]bool{"mysql": *mysql, "postgres": *postgres, "sqlite": *sqlite || *sqllite} {
		if on {
			dialects = append(dialects, name)
		}
	}
	if len(dialects) > 1 {
		sort.Strings(dialects)
		return o, fmt.Errorf("pick one SQL database, got --%s", strings.Join(dialects, " --"))
	}
	if len(dialects) == 1 {
		o.Dialect = dialects[0]
		*modes["sql"] = true
	}
	var chosen []string
	for _, m := range modeFlags {
		if *modes[m] {
			chosen = append(chosen, m)
		}
	}
	if len(chosen) > 1 {
		return o, fmt.Errorf("pick one mode, got -%s", strings.Join(chosen, " -"))
	}
	if len(chosen) == 1 {
		o.Mode = chosen[0]
	}
	if o.Mode == "sql" && o.Dialect == "" {
		return o, errors.New("-sql needs --mysql, --postgres or --sqlite")
	}
	o.Query = strings.TrimSpace(strings.Join(fs.Args(), " "))
	if o.Mode == "" && o.Query != "" {
		return o, fmt.Errorf("unexpected argument %q, run devcli --help", fs.Arg(0))
	}
	return o, nil
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func DefaultTargets() ui.Targets {
	return ui.Targets{
		MySQL:        env("DEVCLI_MYSQL", "mysql://root:devcli@127.0.0.1:3306/devcli"),
		Postgres:     env("DEVCLI_POSTGRES", "postgres://postgres:devcli@127.0.0.1:5432/devcli?sslmode=disable"),
		SQLite:       env("DEVCLI_SQLITE", "devcli.db"),
		Cassandra:    env("DEVCLI_CASSANDRA", "127.0.0.1:9042/devcli"),
		Redis:        env("DEVCLI_REDIS", "redis://127.0.0.1:6380/0"),
		Loki:         env("DEVCLI_LOKI", "http://127.0.0.1:3100"),
		Grafana:      env("DEVCLI_GRAFANA", "http://admin:devcli@127.0.0.1:3000"),
		GrafanaToken: os.Getenv("DEVCLI_GRAFANA_TOKEN"),
		Prometheus:   env("DEVCLI_PROMETHEUS", "http://127.0.0.1:9090"),
	}
}

func BackendFor(o Options, t ui.Targets) (backend.Backend, string) {
	pick := func(b backend.Backend, target string) (backend.Backend, string) {
		if o.Target != "" {
			target = o.Target
		}
		return b, target
	}
	switch o.Mode {
	case "sql":
		switch o.Dialect {
		case "mysql":
			return pick(backend.NewMySQL(t.MySQL), t.MySQL)
		case "postgres":
			return pick(backend.NewPostgres(t.Postgres), t.Postgres)
		}
		return pick(backend.NewSQLite(t.SQLite), t.SQLite)
	case "cassandra":
		return pick(backend.NewCassandra(t.Cassandra), t.Cassandra)
	case "redis":
		return pick(backend.NewRedis(t.Redis), t.Redis)
	case "loki":
		return pick(backend.NewLoki(t.Loki), t.Loki)
	case "grafana":
		return pick(backend.NewGrafana(t.Grafana, t.GrafanaToken), t.Grafana)
	case "prometheus":
		return pick(backend.NewPrometheus(t.Prometheus), t.Prometheus)
	}
	return nil, ""
}

func Main(o Options, t ui.Targets, stdio IO) int {
	color := !o.NoColor && os.Getenv("NO_COLOR") == ""
	switch {
	case o.Help:
		fmt.Fprint(stdio.Out, Colorize(Banner()+Help(), color && stdio.OutTTY))
		return 0
	case o.Version:
		fmt.Fprintln(stdio.Out, "devcli", ui.Version)
		return 0
	case o.Capture != "":
		return report(stdio, color, ui.Capture(t, o.Capture))
	case o.Mode == "":
		app := ui.New(t, nil)
		app.NoDiscover = o.NoDiscover
		return report(stdio, color, app.Run(o.Tab))
	}
	if !o.Quiet && stdio.ErrTTY {
		fmt.Fprint(stdio.Err, Colorize(Banner(), color))
	}
	if needsQuery[o.Mode] && o.Query == "" && !stdio.InTTY && stdio.In != nil {
		data, err := io.ReadAll(stdio.In)
		if err != nil {
			return report(stdio, color, err)
		}
		o.Query = strings.TrimSpace(string(data))
	}
	if needsQuery[o.Mode] && o.Query == "" {
		return report(stdio, color, fmt.Errorf("-%s needs a query as arguments or on stdin, run devcli --help", o.Mode))
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	defer cancel()
	results, err := Execute(ctx, o, t)
	for _, r := range results {
		fmt.Fprint(stdio.Out, Colorize(ui.RenderResult(r, o.JSON, stdio.OutTTY, stdio.Width), color && stdio.OutTTY))
	}
	return report(stdio, color, err)
}

func report(stdio IO, color bool, err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprint(stdio.Err, Colorize("[#ff5c7a::b]✖ devcli:[-::-] [#ff5c7a]"+escape(err.Error())+"[-]\n", color && stdio.ErrTTY))
	return 1
}

func escape(s string) string {
	return tview.Escape(s)
}

func Execute(ctx context.Context, o Options, t ui.Targets) ([]backend.Result, error) {
	switch o.Mode {
	case "ps":
		return processes(ctx, o)
	case "containers":
		return containers(ctx)
	case "threads":
		return threads(ctx, o)
	case "discover":
		return discovered(ctx)
	}
	b, target := BackendFor(o, t)
	if b == nil {
		return nil, fmt.Errorf("unknown mode %q", o.Mode)
	}
	if err := b.Connect(ctx, target); err != nil {
		return nil, fmt.Errorf("connect %s %s: %w", b.Name(), ui.MaskTarget(target), err)
	}
	defer b.Close()
	return b.Execute(ctx, o.Query)
}

func processes(ctx context.Context, o Options) ([]backend.Result, error) {
	procs, err := sys.Processes(ctx, sys.Run)
	if err != nil {
		return nil, err
	}
	sys.SortProcesses(procs, sys.ByCPU)
	procs = sys.FilterProcesses(procs, o.Query)
	r := backend.Result{Title: "processes " + o.Query, Columns: []string{"pid", "ppid", "user", "cpu", "mem", "rss", "time", "name", "command"}}
	for _, p := range procs {
		var rss any = p.RSS
		if !o.JSON {
			rss = ui.BytesLabel(float64(p.RSS))
		}
		r.Rows = append(r.Rows, []any{p.PID, p.PPID, p.User, p.CPU, p.Mem, rss, p.Elapsed, p.Name(), p.Command})
	}
	r.Message = fmt.Sprintf("%d processes", len(r.Rows))
	return []backend.Result{r}, nil
}

func containers(ctx context.Context) ([]backend.Result, error) {
	runtime, err := sys.Runtime()
	if err != nil {
		return nil, err
	}
	list, err := sys.Containers(ctx, sys.Run, runtime)
	if err != nil {
		return nil, err
	}
	r := backend.Result{Title: "containers", Columns: []string{"state", "name", "id", "image", "ports", "status"}}
	for _, c := range list {
		r.Rows = append(r.Rows, []any{c.State, c.Name, c.ID, c.Image, c.Ports, c.Status})
	}
	r.Message = fmt.Sprintf("%d containers", len(r.Rows))
	return []backend.Result{r}, nil
}

func threads(ctx context.Context, o Options) ([]backend.Result, error) {
	list, err := jvm.Discover(ctx, sys.Run)
	if err != nil {
		return nil, err
	}
	if o.Query == "" {
		r := backend.Result{Title: "jvms", Columns: []string{"pid", "language", "main"}}
		for _, p := range list {
			r.Rows = append(r.Rows, []any{p.PID, string(p.Language), p.Main})
		}
		r.Message = fmt.Sprintf("%d JVMs · devcli -threads PID dumps one", len(r.Rows))
		return []backend.Result{r}, nil
	}
	pid, err := strconv.Atoi(o.Query)
	if err != nil {
		return nil, fmt.Errorf("-threads takes a pid, got %q", o.Query)
	}
	lang := jvm.Java
	for _, p := range list {
		if p.PID == pid {
			lang = p.Language
		}
	}
	d, err := jvm.ThreadDump(ctx, sys.Run, pid)
	if err != nil {
		return nil, err
	}
	r := backend.Result{Title: fmt.Sprintf("thread dump %d", pid), Message: fmt.Sprintf("%d threads", len(d.Threads))}
	if o.JSON {
		r.Value = d
	} else {
		r.Text = ui.RenderDump(lang, pid, d, "", func(jvm.Thread) bool { return true })
	}
	return []backend.Result{r}, nil
}

var modeFor = map[string]string{
	"Postgres": "-sql --postgres", "MySQL": "-sql --mysql", "Cassandra": "-cassandra", "Redis": "-redis",
	"Loki": "-loki", "Grafana": "-grafana", "Prometheus": "-prometheus",
}

func discovered(ctx context.Context) ([]backend.Result, error) {
	found, err := discover.Containers(ctx, sys.Run)
	if err != nil {
		return nil, err
	}
	r := backend.Result{Title: "discovered containers", Columns: []string{"kind", "container", "image", "target", "run with"}}
	for _, f := range found {
		r.Rows = append(r.Rows, []any{f.Kind, f.Container, f.Image, ui.MaskTarget(f.Target), "devcli " + modeFor[f.Kind] + " -target URL"})
	}
	r.Message = fmt.Sprintf("%d containers", len(r.Rows))
	return []backend.Result{r}, nil
}

func Banner() string {
	var b strings.Builder
	lines := ui.BannerLines()
	width := len([]rune(lines[0]))
	b.WriteString("\n")
	for _, line := range lines {
		b.WriteString("  ")
		for col, r := range []rune(line) {
			b.WriteString("[" + theme.Hex(ui.BannerColor(col, width, 0)) + "::b]" + string(r))
		}
		b.WriteString("[-::-]\n")
	}
	b.WriteString("  [" + theme.Dim + "]⚡ v" + ui.Version + " · " + ui.Tagline + "[-]\n\n")
	return b.String()
}
