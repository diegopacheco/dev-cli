package backend

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

type Loki struct {
	target string
	api    *httpAPI
	Range  time.Duration
	Limit  int
	Now    func() time.Time
}

func NewLoki(target string) *Loki {
	return &Loki{target: target, Range: time.Hour, Limit: 200, Now: time.Now}
}

func (l *Loki) Name() string                  { return "Loki" }
func (l *Loki) Language() syntax.Language     { return syntax.LogQL }
func (l *Loki) DefaultTarget() string         { return l.target }
func (l *Loki) RunsOnEnter(input string) bool { return true }
func (l *Loki) Close() error                  { l.api = nil; return nil }

func (l *Loki) Connect(ctx context.Context, target string) error {
	api, err := newHTTPAPI(target, "")
	if err != nil {
		return err
	}
	if _, err := api.do(ctx, "GET", "/loki/api/v1/labels", nil, nil); err != nil {
		return err
	}
	l.api, l.target = api, target
	return nil
}

func (l *Loki) Execute(ctx context.Context, input string) ([]Result, error) {
	if l.api == nil {
		return nil, errors.New("not connected")
	}
	line := strings.TrimSpace(strings.Join(Lines(input), " "))
	start := time.Now()
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil, nil
	}
	if strings.EqualFold(line, "all") {
		return l.all(ctx)
	}
	if e, ok := lookupFunction(lokiFunctions, line); ok {
		return functionHint(line, e), nil
	}
	var r Result
	var err error
	switch strings.ToLower(fields[0]) {
	case ":range":
		if len(fields) < 2 {
			return nil, errors.New("usage: :range 30m")
		}
		d, perr := time.ParseDuration(fields[1])
		if perr != nil {
			return nil, perr
		}
		l.Range = d
		r.Message = "range set to " + d.String()
	case ":limit":
		if len(fields) < 2 {
			return nil, errors.New("usage: :limit 100")
		}
		n, perr := strconv.Atoi(fields[1])
		if perr != nil {
			return nil, perr
		}
		l.Limit = n
		r.Message = fmt.Sprintf("limit set to %d", n)
	case "labels":
		r, err = l.names(ctx, "/loki/api/v1/labels", "label")
	case "values":
		if len(fields) < 2 {
			return nil, errors.New("usage: values <label>")
		}
		r, err = l.names(ctx, "/loki/api/v1/label/"+url.PathEscape(fields[1])+"/values", fields[1])
	default:
		r, err = l.query(ctx, line)
		if err != nil && strings.Contains(err.Error(), "parse error") {
			err = fmt.Errorf("%w · a LogQL query starts with a stream selector like {app=\"api\"}, run all to list labels and ready queries", err)
		}
	}
	if err != nil {
		return nil, err
	}
	r.Title = line
	r.Elapsed = time.Since(start)
	return []Result{r}, nil
}

var lokiCommands = [][2]string{
	{"all", "list commands, ready queries, labels, streams, pipeline stages and functions"},
	{`{label="value"}`, "log lines of the matching streams, newest first"},
	{"labels", "label names in the time range"},
	{"values <label>", "values of one label"},
	{":range 30m", "time range of every query (default 1h)"},
	{":limit 100", "maximum log lines per query (default 200)"},
}

func (l *Loki) all(ctx context.Context) ([]Result, error) {
	v, err := l.api.do(ctx, "GET", "/loki/api/v1/labels", l.window(), nil)
	if err != nil {
		return nil, err
	}
	values := map[string][]string{}
	labels := Result{Title: "labels", Columns: []string{"label", "values"}}
	var names []string
	for _, n := range list(field(v, "data")) {
		names = append(names, str(n))
	}
	for i, name := range names {
		preview := ""
		if i < 30 {
			if vals, err := l.api.do(ctx, "GET", "/loki/api/v1/label/"+url.PathEscape(name)+"/values", l.window(), nil); err == nil {
				for _, x := range list(field(vals, "data")) {
					values[name] = append(values[name], str(x))
				}
				preview = previewValues(values[name], 10)
			}
		}
		labels.Rows = append(labels.Rows, []any{name, preview})
	}
	labels.Message = fmt.Sprintf("%d labels in the last %s", len(names), l.Range)
	streams := Result{Title: "streams", Columns: []string{"stream"}}
	if len(names) > 0 {
		q := l.window()
		for _, name := range names {
			q.Add("match[]", "{"+name+`=~".+"}`)
		}
		if s, err := l.api.do(ctx, "GET", "/loki/api/v1/series", q, nil); err == nil {
			for _, st := range list(field(s, "data")) {
				if len(streams.Rows) < MaxRows {
					streams.Rows = append(streams.Rows, []any{labelString(st)})
				}
			}
		}
	}
	streams.Message = fmt.Sprintf("%d streams", len(streams.Rows))
	ready := Result{Title: "ready queries", Columns: []string{"query", "what it shows"}, Wide: true}
	add := func(q, desc string) { ready.Rows = append(ready.Rows, []any{q, desc}) }
	if len(names) > 0 {
		label := names[0]
		for _, preferred := range []string{"app", "service_name", "job", "container"} {
			if len(values[preferred]) > 0 {
				label = preferred
				break
			}
		}
		selector := "{" + label + `=~".+"}`
		if vals := values[label]; len(vals) > 0 {
			selector = "{" + label + `="` + vals[0] + `"}`
		}
		add(selector, "log lines of one stream")
		add(selector+` |= "error"`, "lines containing error")
		add(selector+" | json", "lines parsed as JSON into labels")
		add("sum by ("+label+") (count_over_time({"+label+`=~".+"}[5m]))`, "lines per "+label+" in 5 minutes")
		add("topk(3, sum by ("+label+") (rate({"+label+`=~".+"}[1m])))`, "busiest "+label+" values per second")
		if len(values["level"]) > 0 {
			add(`sum by (level) (count_over_time({level=~".+"}[15m]))`, "lines per level")
		}
	}
	ready.Message = "⌘K then type ready to load one into the editor"
	return []Result{
		commandResult("commands", lokiCommands),
		ready,
		labels,
		streams,
		entryResult("pipeline stages", lokiStages),
		entryResult("functions", lokiFunctions),
	}, nil
}

func (l *Loki) names(ctx context.Context, path, column string) (Result, error) {
	v, err := l.api.do(ctx, "GET", path, l.window(), nil)
	if err != nil {
		return Result{}, err
	}
	r := Result{Columns: []string{column}, Value: field(v, "data")}
	for _, n := range list(field(v, "data")) {
		r.Rows = append(r.Rows, []any{n})
	}
	r.Message = fmt.Sprintf("%d values", len(r.Rows))
	return r, nil
}

func (l *Loki) window() url.Values {
	now := l.Now()
	q := url.Values{}
	q.Set("start", strconv.FormatInt(now.Add(-l.Range).UnixNano(), 10))
	q.Set("end", strconv.FormatInt(now.UnixNano(), 10))
	return q
}

func (l *Loki) query(ctx context.Context, logql string) (Result, error) {
	q := l.window()
	q.Set("query", logql)
	q.Set("limit", strconv.Itoa(l.Limit))
	q.Set("direction", "backward")
	v, err := l.api.do(ctx, "GET", "/loki/api/v1/query_range", q, nil)
	if err != nil {
		return Result{}, err
	}
	return ShapeLoki(field(v, "data")), nil
}

type logEntry struct {
	ns     int64
	stream any
	line   string
}

func labelString(v any) string {
	obj, _ := v.(syntax.Object)
	parts := make([]string, 0, len(obj))
	for _, p := range obj {
		parts = append(parts, fmt.Sprintf("%s=%q", p.Key, str(p.Value)))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func ShapeLoki(data any) Result {
	resultType := str(field(data, "resultType"))
	result := list(field(data, "result"))
	switch resultType {
	case "streams":
		var entries []logEntry
		for _, s := range result {
			labels := field(s, "stream")
			for _, pair := range list(field(s, "values")) {
				p := list(pair)
				if len(p) < 2 {
					continue
				}
				ns, _ := strconv.ParseInt(str(p[0]), 10, 64)
				entries = append(entries, logEntry{ns, labels, str(p[1])})
			}
		}
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].ns > entries[j].ns })
		r := Result{}
		values := []any{}
		for _, e := range entries {
			ts := time.Unix(0, e.ns).Format("2006-01-02 15:04:05.000")
			labels, _ := e.stream.(syntax.Object)
			r.Logs = append(r.Logs, LogLine{Time: ts, Labels: labels, Line: e.line})
			values = append(values, syntax.Object{{Key: "time", Value: ts}, {Key: "labels", Value: e.stream}, {Key: "line", Value: e.line}})
		}
		r.Value = values
		r.Message = fmt.Sprintf("%d lines from %d streams", len(entries), len(result))
		return r
	case "matrix", "vector", "scalar":
		return ShapeSeries(data)
	}
	return Result{Value: data, Message: resultType}
}

func ShapeSeries(data any) Result {
	resultType := str(field(data, "resultType"))
	result := list(field(data, "result"))
	if resultType == "scalar" {
		pair := list(field(data, "result"))
		if len(pair) == 2 {
			return Result{Columns: []string{"value"}, Rows: [][]any{{pair[1]}}, Value: data, Message: "scalar"}
		}
	}
	r := Result{Columns: []string{"series", "samples", "last"}, Value: result}
	for _, s := range result {
		points := list(field(s, "values"))
		last := list(field(s, "value"))
		if len(points) > 0 {
			last = list(points[len(points)-1])
		}
		lastValue := ""
		if len(last) == 2 {
			lastValue = str(last[1])
		}
		count := len(points)
		if resultType == "vector" {
			count = 1
		}
		r.Rows = append(r.Rows, []any{labelString(field(s, "metric")), count, lastValue})
	}
	r.Message = fmt.Sprintf("%d series", len(result))
	return r
}

func (l *Loki) Words(ctx context.Context) []string {
	if l.api == nil {
		return nil
	}
	v, err := l.api.do(ctx, "GET", "/loki/api/v1/labels", l.window(), nil)
	if err != nil {
		return nil
	}
	out := []string{"all", "labels", "values", ":range", ":limit"}
	for i, n := range list(field(v, "data")) {
		name := str(n)
		out = append(out, name)
		if i >= 20 {
			continue
		}
		vals, err := l.api.do(ctx, "GET", "/loki/api/v1/label/"+url.PathEscape(name)+"/values", l.window(), nil)
		if err != nil {
			continue
		}
		for j, val := range list(field(vals, "data")) {
			if j >= 50 {
				break
			}
			out = append(out, str(val))
		}
	}
	return Unique(out)
}
