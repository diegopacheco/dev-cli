package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

type Grafana struct {
	target string
	token  string
	api    *httpAPI
}

func NewGrafana(target, token string) *Grafana {
	return &Grafana{target: target, token: token}
}

func (g *Grafana) Name() string                  { return "Grafana" }
func (g *Grafana) Language() syntax.Language     { return syntax.Grafana }
func (g *Grafana) DefaultTarget() string         { return g.target }
func (g *Grafana) RunsOnEnter(input string) bool { return true }
func (g *Grafana) Close() error                  { g.api = nil; return nil }

func (g *Grafana) Connect(ctx context.Context, target string) error {
	api, err := newHTTPAPI(target, g.token)
	if err != nil {
		return err
	}
	if _, err := api.do(ctx, "GET", "/api/search", url.Values{"limit": {"1"}}, nil); err != nil {
		return err
	}
	g.api, g.target = api, target
	return nil
}

const grafanaHelp = "commands: all | health | search [text] | dashboard <uid> | chart <uid> [panel] | datasources | folders | alerts | annotations | query <datasource-uid> <expr> | get </api/path>"

func (g *Grafana) Execute(ctx context.Context, input string) ([]Result, error) {
	if g.api == nil {
		return nil, errors.New("not connected")
	}
	var results []Result
	for _, line := range Lines(input) {
		start := time.Now()
		if strings.EqualFold(line, "all") {
			all, err := g.all(ctx)
			if err != nil {
				return results, fmt.Errorf("all: %w", err)
			}
			results = append(results, all...)
			continue
		}
		if cmd, rest, _ := strings.Cut(line, " "); strings.EqualFold(cmd, "chart") {
			charts, err := g.chart(ctx, strings.TrimSpace(rest))
			if err != nil {
				return results, fmt.Errorf("%s: %w", line, err)
			}
			results = append(results, charts...)
			continue
		}
		r, err := g.run(ctx, line)
		if err != nil {
			return results, fmt.Errorf("%s: %w", line, err)
		}
		r.Title = line
		r.Elapsed = time.Since(start)
		results = append(results, r)
	}
	return results, nil
}

func (g *Grafana) run(ctx context.Context, line string) (Result, error) {
	cmd, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(cmd) {
	case "health":
		v, err := g.api.do(ctx, "GET", "/api/health", nil, nil)
		return Result{Value: v, Message: str(field(v, "database"))}, err
	case "search":
		v, err := g.api.do(ctx, "GET", "/api/search", url.Values{"query": {rest}}, nil)
		return table(v, err, []string{"type", "uid", "title", "folderTitle", "url"})
	case "dashboard":
		if rest == "" {
			return Result{}, errors.New("usage: dashboard <uid>")
		}
		v, err := g.api.do(ctx, "GET", "/api/dashboards/uid/"+url.PathEscape(rest), nil, nil)
		if err != nil {
			return Result{}, err
		}
		r := Result{Columns: []string{"id", "type", "title", "datasource", "expr"}, Value: v}
		for _, p := range list(field(v, "dashboard", "panels")) {
			expr := ""
			if targets := list(field(p, "targets")); len(targets) > 0 {
				expr = str(field(targets[0], "expr"))
			}
			r.Rows = append(r.Rows, []any{field(p, "id"), str(field(p, "type")), str(field(p, "title")), str(field(p, "datasource", "uid")), expr})
		}
		r.Message = fmt.Sprintf("%s: %d panels · chart %s draws them", str(field(v, "dashboard", "title")), len(r.Rows), rest)
		return r, nil
	case "datasources":
		v, err := g.api.do(ctx, "GET", "/api/datasources", nil, nil)
		return table(v, err, []string{"uid", "name", "type", "url", "isDefault"})
	case "folders":
		v, err := g.api.do(ctx, "GET", "/api/folders", nil, nil)
		return table(v, err, []string{"uid", "title"})
	case "alerts":
		v, err := g.api.do(ctx, "GET", "/api/v1/provisioning/alert-rules", nil, nil)
		return table(v, err, []string{"uid", "title", "folderUID", "condition"})
	case "annotations":
		v, err := g.api.do(ctx, "GET", "/api/annotations", url.Values{"limit": {"50"}}, nil)
		return table(v, err, []string{"id", "dashboardUID", "text", "tags"})
	case "query":
		ds, expr, ok := strings.Cut(rest, " ")
		if !ok || strings.TrimSpace(expr) == "" {
			return Result{}, errors.New("usage: query <datasource-uid> <expr>")
		}
		return g.query(ctx, ds, strings.TrimSpace(expr))
	case "get":
		if !strings.HasPrefix(rest, "/") {
			return Result{}, errors.New("usage: get /api/path")
		}
		v, err := g.api.do(ctx, "GET", rest, nil, nil)
		return Result{Value: v}, err
	case "help":
		return Result{Message: grafanaHelp}, nil
	}
	return Result{}, errors.New(grafanaHelp)
}

var grafanaCommands = [][2]string{
	{"all", "list commands, ready commands, health, datasources, folders, dashboards and alert rules"},
	{"health", "server version and database state"},
	{"search [text]", "dashboards and folders"},
	{"dashboard <uid>", "panels of a dashboard with their queries; Ctrl-T for the full JSON"},
	{"chart <uid> [panel]", "draw the panels of a dashboard in the terminal: line charts, stats, gauges, logs and tables; panel is an id or part of a title"},
	{"datasources", "configured datasources"},
	{"folders", "dashboard folders"},
	{"alerts", "provisioned alert rules"},
	{"annotations", "latest 50 annotations"},
	{"query <datasource-uid> <expr>", "run an expression through a datasource (last hour); numeric results draw a line chart"},
	{"get /api/<path>", "any GET endpoint of the Grafana HTTP API"},
}

func (g *Grafana) all(ctx context.Context) ([]Result, error) {
	health, err := g.run(ctx, "health")
	if err != nil {
		return nil, err
	}
	health.Title = "health"
	out := []Result{commandResult("commands", grafanaCommands)}
	ready := Result{Title: "ready commands", Columns: []string{"command", "what it shows"}, Wide: true}
	sections := []struct{ title, command string }{
		{"datasources", "datasources"},
		{"folders", "folders"},
		{"dashboards", "search"},
		{"alert rules", "alerts"},
	}
	var parts []Result
	for _, s := range sections {
		r, err := g.run(ctx, s.command)
		if err != nil {
			r = Result{Message: "unavailable: " + err.Error()}
		}
		r.Title = s.title
		parts = append(parts, r)
		switch s.command {
		case "datasources":
			for _, row := range r.Rows {
				uid, kind := str(row[0]), str(row[2])
				switch kind {
				case "loki":
					ready.Rows = append(ready.Rows, []any{"query " + uid + ` {service_name=~".+"}`, "log lines through the " + str(row[1]) + " datasource"})
				case "prometheus":
					ready.Rows = append(ready.Rows, []any{"query " + uid + " up", "scrape health through the " + str(row[1]) + " datasource"})
				}
			}
		case "search":
			for _, row := range r.Rows {
				if str(row[0]) == "dash-db" && len(ready.Rows) < 12 {
					ready.Rows = append(ready.Rows,
						[]any{"chart " + str(row[1]), "draw every panel of " + str(row[2])},
						[]any{"dashboard " + str(row[1]), "panels of " + str(row[2])},
					)
				}
			}
		}
	}
	ready.Message = "⌘K then type ready to load one into the editor"
	out = append(out, ready, health)
	return append(out, parts...), nil
}

func table(v any, err error, columns []string) (Result, error) {
	if err != nil {
		return Result{}, err
	}
	r := Result{Columns: columns, Value: v}
	for _, item := range list(v) {
		row := make([]any, len(columns))
		for i, c := range columns {
			row[i] = field(item, c)
		}
		r.Rows = append(r.Rows, row)
	}
	r.Message = fmt.Sprintf("%d items", len(r.Rows))
	return r, nil
}

func (g *Grafana) query(ctx context.Context, ds, expr string) (Result, error) {
	body, _ := json.Marshal(map[string]any{
		"from": "now-1h",
		"to":   "now",
		"queries": []map[string]any{{
			"refId":         "A",
			"datasource":    map[string]string{"uid": ds},
			"expr":          expr,
			"queryType":     "range",
			"maxLines":      200,
			"intervalMs":    intervalMs("now-1h", "now"),
			"maxDataPoints": chartPoints,
		}},
	})
	v, err := g.api.do(ctx, "POST", "/api/ds/query", nil, body)
	if err != nil {
		return Result{}, err
	}
	r := ShapeFrames(v)
	if series := FrameSeries(list(field(v, "results", "A", "frames"))); len(series) > 0 {
		r.Chart = &Chart{Kind: "timeseries", Series: series}
	}
	return r, nil
}

func ShapeFrames(v any) Result {
	r := framesTable(list(field(v, "results", "A", "frames")))
	r.Value = v
	if e := str(field(v, "results", "A", "error")); e != "" {
		r.Message = e
	} else {
		r.Message = fmt.Sprintf("%d rows", len(r.Rows))
	}
	return r
}

func (g *Grafana) Words(ctx context.Context) []string {
	if g.api == nil {
		return nil
	}
	out := []string{"all", "health", "search", "dashboard", "chart", "datasources", "folders", "alerts", "annotations", "query", "get", "/api/health", "/api/search", "/api/datasources", "/api/folders", "/api/org", "/api/users"}
	if v, err := g.api.do(ctx, "GET", "/api/search", nil, nil); err == nil {
		for _, item := range list(v) {
			out = append(out, str(field(item, "uid")))
		}
	}
	if v, err := g.api.do(ctx, "GET", "/api/datasources", nil, nil); err == nil {
		for _, item := range list(v) {
			out = append(out, str(field(item, "uid")))
		}
	}
	return Unique(out)
}

func (g *Grafana) chart(ctx context.Context, args string) ([]Result, error) {
	uid, pick, _ := strings.Cut(args, " ")
	pick = strings.ToLower(strings.TrimSpace(pick))
	if uid == "" {
		return nil, errors.New("usage: chart <dashboard-uid> [panel id or title]")
	}
	v, err := g.api.do(ctx, "GET", "/api/dashboards/uid/"+url.PathEscape(uid), nil, nil)
	if err != nil {
		return nil, err
	}
	dash := field(v, "dashboard")
	from, to := str(field(dash, "time", "from")), str(field(dash, "time", "to"))
	if from == "" || to == "" {
		from, to = "now-1h", "now"
	}
	var out []Result
	for _, p := range flattenPanels(list(field(dash, "panels"))) {
		if pick != "" && str(field(p, "id")) != pick && !strings.Contains(strings.ToLower(str(field(p, "title"))), pick) {
			continue
		}
		out = append(out, g.panel(ctx, p, from, to))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no panel of %s matches %q", uid, pick)
	}
	return out, nil
}

func flattenPanels(panels []any) []any {
	var out []any
	for _, p := range panels {
		if str(field(p, "type")) == "row" {
			out = append(out, list(field(p, "panels"))...)
			continue
		}
		out = append(out, p)
	}
	return out
}

func panelQuery(target, panelDatasource any, interval int64) syntax.Object {
	q := syntax.Object{}
	obj, _ := target.(syntax.Object)
	for _, pair := range obj {
		switch pair.Key {
		case "datasource", "intervalMs", "maxDataPoints":
			continue
		}
		q = append(q, pair)
	}
	ds := field(target, "datasource")
	if str(field(ds, "uid")) == "" {
		ds = panelDatasource
	}
	return append(q, syntax.Pair{Key: "datasource", Value: ds}, syntax.Pair{Key: "intervalMs", Value: interval}, syntax.Pair{Key: "maxDataPoints", Value: chartPoints})
}

func (g *Grafana) panel(ctx context.Context, p any, from, to string) Result {
	start := time.Now()
	kind := str(field(p, "type"))
	r := Result{Title: str(field(p, "title"))}
	if r.Title == "" {
		r.Title = kind + " panel " + str(field(p, "id"))
	}
	var queries []any
	for _, t := range list(field(p, "targets")) {
		if field(t, "hide") != true {
			queries = append(queries, panelQuery(t, field(p, "datasource"), intervalMs(from, to)))
		}
	}
	if len(queries) == 0 {
		r.Message = kind + " panel has no queries to draw"
		return r
	}
	body := syntax.Object{{Key: "from", Value: from}, {Key: "to", Value: to}, {Key: "queries", Value: queries}}
	v, err := g.api.do(ctx, "POST", "/api/ds/query", nil, []byte(syntax.Scalar(body)))
	r.Elapsed = time.Since(start)
	if err != nil {
		r.Message = "unavailable: " + err.Error()
		return r
	}
	r.Value = v
	var frames []any
	var errs []string
	results, _ := field(v, "results").(syntax.Object)
	for _, res := range results {
		frames = append(frames, list(field(res.Value, "frames"))...)
		if e := str(field(res.Value, "error")); e != "" {
			errs = append(errs, e)
		}
	}
	span := from + " to " + to
	series := FrameSeries(frames)
	logs := FrameLogs(frames)
	switch {
	case len(series) > 0 && kind != "table":
		limit := number(field(p, "fieldConfig", "defaults", "max"))
		if math.IsNaN(limit) {
			limit = 0
		}
		r.Chart = &Chart{Kind: chartKind(kind), Unit: str(field(p, "fieldConfig", "defaults", "unit")), Max: limit, Series: series}
		r.Message = fmt.Sprintf("%s · %d series · %s", kind, len(series), span)
	case len(logs) > 0:
		r.Logs = logs
		r.Message = fmt.Sprintf("%s · %d lines · %s", kind, len(logs), span)
	default:
		t := framesTable(frames)
		r.Columns, r.Rows = t.Columns, t.Rows
		r.Message = fmt.Sprintf("%s · %d rows · %s", kind, len(t.Rows), span)
	}
	if len(errs) > 0 {
		r.Message = "error: " + strings.Join(errs, "; ")
	}
	return r
}
