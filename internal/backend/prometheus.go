package backend

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

type Prometheus struct {
	target string
	api    *httpAPI
	Range  time.Duration
	Step   time.Duration
	Now    func() time.Time
}

func NewPrometheus(target string) *Prometheus {
	return &Prometheus{target: target, Now: time.Now}
}

func (p *Prometheus) Name() string                  { return "Prometheus" }
func (p *Prometheus) Language() syntax.Language     { return syntax.PromQL }
func (p *Prometheus) DefaultTarget() string         { return p.target }
func (p *Prometheus) RunsOnEnter(input string) bool { return true }
func (p *Prometheus) Close() error                  { p.api = nil; return nil }

func (p *Prometheus) Connect(ctx context.Context, target string) error {
	api, err := newHTTPAPI(target, "")
	if err != nil {
		return err
	}
	if _, err := api.do(ctx, "GET", "/api/v1/status/buildinfo", nil, nil); err != nil {
		return err
	}
	p.api, p.target = api, target
	return nil
}

const prometheusHelp = "commands: <promql> | :range 30m | :step 15s | :range off | metrics [text] | labels | values <label> | targets | alerts | rules"

func (p *Prometheus) Execute(ctx context.Context, input string) ([]Result, error) {
	if p.api == nil {
		return nil, errors.New("not connected")
	}
	var results []Result
	for _, line := range Lines(input) {
		start := time.Now()
		r, err := p.run(ctx, line)
		if err != nil {
			return results, fmt.Errorf("%s: %w", line, err)
		}
		r.Title = line
		r.Elapsed = time.Since(start)
		results = append(results, r)
	}
	return results, nil
}

func (p *Prometheus) run(ctx context.Context, line string) (Result, error) {
	fields := strings.Fields(line)
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	switch strings.ToLower(fields[0]) {
	case ":range":
		if arg == "off" {
			p.Range = 0
			return Result{Message: "instant queries"}, nil
		}
		d, err := time.ParseDuration(arg)
		if err != nil {
			return Result{}, errors.New("usage: :range 30m | :range off")
		}
		p.Range = d
		return Result{Message: "range queries over " + d.String()}, nil
	case ":step":
		d, err := time.ParseDuration(arg)
		if err != nil {
			return Result{}, errors.New("usage: :step 15s")
		}
		p.Step = d
		return Result{Message: "step " + d.String()}, nil
	case "metrics":
		v, err := p.api.do(ctx, "GET", "/api/v1/label/__name__/values", nil, nil)
		return names(v, err, "metric", arg)
	case "labels":
		v, err := p.api.do(ctx, "GET", "/api/v1/labels", nil, nil)
		return names(v, err, "label", "")
	case "values":
		if arg == "" {
			return Result{}, errors.New("usage: values <label>")
		}
		v, err := p.api.do(ctx, "GET", "/api/v1/label/"+url.PathEscape(arg)+"/values", nil, nil)
		return names(v, err, arg, "")
	case "targets":
		v, err := p.api.do(ctx, "GET", "/api/v1/targets", nil, nil)
		if err != nil {
			return Result{}, err
		}
		r := Result{Columns: []string{"job", "instance", "health", "lastScrape", "lastError"}, Value: field(v, "data")}
		for _, t := range list(field(v, "data", "activeTargets")) {
			r.Rows = append(r.Rows, []any{str(field(t, "labels", "job")), str(field(t, "labels", "instance")), str(field(t, "health")), str(field(t, "lastScrape")), str(field(t, "lastError"))})
		}
		r.Message = fmt.Sprintf("%d targets", len(r.Rows))
		return r, nil
	case "alerts":
		v, err := p.api.do(ctx, "GET", "/api/v1/alerts", nil, nil)
		if err != nil {
			return Result{}, err
		}
		alerts := list(field(v, "data", "alerts"))
		return Result{Value: alerts, Message: fmt.Sprintf("%d alerts", len(alerts))}, nil
	case "rules":
		v, err := p.api.do(ctx, "GET", "/api/v1/rules", nil, nil)
		return Result{Value: field(v, "data")}, err
	case "help":
		return Result{Message: prometheusHelp}, nil
	}
	return p.query(ctx, line)
}

func names(v any, err error, column, filter string) (Result, error) {
	if err != nil {
		return Result{}, err
	}
	r := Result{Columns: []string{column}}
	for _, n := range list(field(v, "data")) {
		if filter == "" || strings.Contains(str(n), filter) {
			r.Rows = append(r.Rows, []any{n})
		}
	}
	r.Message = fmt.Sprintf("%d values", len(r.Rows))
	return r, nil
}

func (p *Prometheus) query(ctx context.Context, promql string) (Result, error) {
	now := p.Now()
	q := url.Values{"query": {promql}}
	path := "/api/v1/query"
	q.Set("time", strconv.FormatInt(now.Unix(), 10))
	if p.Range > 0 {
		step := p.Step
		if step <= 0 {
			step = max(time.Second, p.Range/120)
		}
		path = "/api/v1/query_range"
		q.Del("time")
		q.Set("start", strconv.FormatInt(now.Add(-p.Range).Unix(), 10))
		q.Set("end", strconv.FormatInt(now.Unix(), 10))
		q.Set("step", strconv.FormatFloat(step.Seconds(), 'f', -1, 64))
	}
	v, err := p.api.do(ctx, "GET", path, q, nil)
	if err != nil {
		return Result{}, err
	}
	return ShapeSeries(field(v, "data")), nil
}

func (p *Prometheus) Words(ctx context.Context) []string {
	if p.api == nil {
		return nil
	}
	out := []string{"metrics", "labels", "values", "targets", "alerts", "rules", ":range", ":step"}
	for _, path := range []string{"/api/v1/label/__name__/values", "/api/v1/labels"} {
		v, err := p.api.do(ctx, "GET", path, nil, nil)
		if err != nil {
			continue
		}
		for i, n := range list(field(v, "data")) {
			if i >= 3000 {
				break
			}
			out = append(out, str(n))
		}
	}
	return Unique(out)
}
