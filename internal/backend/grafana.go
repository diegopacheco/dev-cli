package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const grafanaHelp = "commands: health | search [text] | dashboard <uid> | datasources | folders | alerts | annotations | query <datasource-uid> <expr> | get </api/path>"

func (g *Grafana) Execute(ctx context.Context, input string) ([]Result, error) {
	if g.api == nil {
		return nil, errors.New("not connected")
	}
	var results []Result
	for _, line := range Lines(input) {
		start := time.Now()
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
		r.Message = fmt.Sprintf("%s: %d panels", str(field(v, "dashboard", "title")), len(r.Rows))
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
			"refId":      "A",
			"datasource": map[string]string{"uid": ds},
			"expr":       expr,
			"queryType":  "range",
			"maxLines":   200,
		}},
	})
	v, err := g.api.do(ctx, "POST", "/api/ds/query", nil, body)
	if err != nil {
		return Result{}, err
	}
	return ShapeFrames(v), nil
}

func ShapeFrames(v any) Result {
	r := Result{Value: v}
	for _, frame := range list(field(v, "results", "A", "frames")) {
		fields := list(field(frame, "schema", "fields"))
		values := list(field(frame, "data", "values"))
		if r.Columns == nil {
			for _, f := range fields {
				r.Columns = append(r.Columns, str(field(f, "name")))
			}
		}
		if len(values) != len(r.Columns) || len(values) == 0 {
			continue
		}
		for i := range list(values[0]) {
			row := make([]any, len(values))
			for c := range values {
				if col := list(values[c]); i < len(col) {
					row[c] = col[i]
				}
			}
			r.Rows = append(r.Rows, row)
		}
	}
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
	out := []string{"health", "search", "dashboard", "datasources", "folders", "alerts", "annotations", "query", "get", "/api/health", "/api/search", "/api/datasources", "/api/folders", "/api/org", "/api/users"}
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
