package backend

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

const chartPoints = 300

type Series struct {
	Name   string
	Times  []int64
	Values []float64
	Step   int64
}

type Chart struct {
	Kind   string
	Unit   string
	Max    float64
	Series []Series
}

func number(v any) float64 {
	switch x := v.(type) {
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f
		}
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f
		}
	}
	return math.NaN()
}

func FrameSeries(frames []any) []Series {
	var out []Series
	for _, frame := range frames {
		fields := list(field(frame, "schema", "fields"))
		values := list(field(frame, "data", "values"))
		timeAt := -1
		for i, f := range fields {
			if str(field(f, "type")) == "time" && i < len(values) {
				timeAt = i
				break
			}
		}
		if timeAt < 0 {
			continue
		}
		times := list(values[timeAt])
		step := int64(max(0, number(field(fields[timeAt], "config", "interval"))))
		for i, f := range fields {
			if str(field(f, "type")) != "number" || i >= len(values) {
				continue
			}
			s := Series{Name: seriesName(frame, f), Step: step}
			for j, v := range list(values[i]) {
				if j >= len(times) {
					break
				}
				t := number(times[j])
				if math.IsNaN(t) {
					continue
				}
				s.Times = append(s.Times, int64(t))
				s.Values = append(s.Values, number(v))
			}
			out = append(out, s)
		}
	}
	return out
}

func seriesName(frame, f any) string {
	if name := str(field(f, "config", "displayNameFromDS")); name != "" {
		return name
	}
	if labels, ok := field(f, "labels").(syntax.Object); ok && len(labels) > 0 {
		parts := make([]string, len(labels))
		for i, p := range labels {
			parts[i] = p.Key + "=" + str(p.Value)
		}
		return strings.Join(parts, ", ")
	}
	if name := str(field(frame, "schema", "name")); name != "" {
		return name
	}
	return str(field(f, "name"))
}

func FrameLogs(frames []any) []LogLine {
	var out []LogLine
	for _, frame := range frames {
		at := map[string]int{}
		for i, f := range list(field(frame, "schema", "fields")) {
			at[str(field(f, "name"))] = i
		}
		values := list(field(frame, "data", "values"))
		lineAt, okLine := at["Line"]
		timeAt, okTime := at["Time"]
		if !okLine || !okTime || lineAt >= len(values) || timeAt >= len(values) {
			continue
		}
		times := list(values[timeAt])
		var labels []any
		if i, ok := at["labels"]; ok && i < len(values) {
			labels = list(values[i])
		}
		for j, line := range list(values[lineAt]) {
			if j >= len(times) {
				break
			}
			l := LogLine{Time: time.UnixMilli(int64(number(times[j]))).Format("2006-01-02 15:04:05.000"), Line: str(line)}
			if j < len(labels) {
				l.Labels, _ = labels[j].(syntax.Object)
			}
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	return out
}

func framesTable(frames []any) Result {
	var r Result
	for _, frame := range frames {
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
	return r
}

var relativeUnits = map[byte]time.Duration{'s': time.Second, 'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}

func relativeAgo(expr string) (time.Duration, bool) {
	if expr == "now" {
		return 0, true
	}
	rest, ok := strings.CutPrefix(expr, "now-")
	if !ok || len(rest) < 2 {
		return 0, false
	}
	unit, found := relativeUnits[rest[len(rest)-1]]
	n, err := strconv.Atoi(rest[:len(rest)-1])
	if !found || err != nil {
		return 0, false
	}
	return time.Duration(n) * unit, true
}

func intervalMs(from, to string) int64 {
	start, okFrom := relativeAgo(from)
	end, okTo := relativeAgo(to)
	if !okFrom || !okTo || start <= end {
		return 15000
	}
	return max(1000, (start-end).Milliseconds()/chartPoints)
}

func chartKind(panelType string) string {
	switch panelType {
	case "stat":
		return "stat"
	case "gauge", "bargauge", "barchart", "piechart":
		return "gauge"
	}
	return "timeseries"
}
