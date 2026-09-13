package backend

import (
	"fmt"
	"sort"
	"strings"
)

type entry struct {
	name  string
	usage string
	desc  string
}

var promFunctions = []entry{
	{"rate", "rate(counter[5m])", "per-second increase of a counter over the range"},
	{"irate", "irate(counter[5m])", "per-second rate from the last two samples"},
	{"increase", "increase(counter[1h])", "total increase of a counter over the range"},
	{"delta", "delta(gauge[10m])", "difference between first and last value of a gauge"},
	{"idelta", "idelta(gauge[5m])", "difference between the last two samples"},
	{"deriv", "deriv(gauge[10m])", "per-second derivative by linear regression"},
	{"changes", "changes(metric[1h])", "how many times the value changed"},
	{"resets", "resets(counter[1h])", "how many times a counter reset"},
	{"avg_over_time", "avg_over_time(metric[5m])", "average of each series over the range"},
	{"sum_over_time", "sum_over_time(metric[5m])", "sum of each series over the range"},
	{"min_over_time", "min_over_time(metric[5m])", "minimum of each series over the range"},
	{"max_over_time", "max_over_time(metric[5m])", "maximum of each series over the range"},
	{"count_over_time", "count_over_time(metric[5m])", "number of samples over the range"},
	{"last_over_time", "last_over_time(metric[5m])", "most recent sample in the range"},
	{"quantile_over_time", "quantile_over_time(0.95, metric[5m])", "quantile of each series over the range"},
	{"absent_over_time", "absent_over_time(metric[5m])", "1 when the series has no samples in the range"},
	{"predict_linear", "predict_linear(gauge[1h], 3600)", "value predicted seconds ahead"},
	{"histogram_quantile", "histogram_quantile(0.95, sum by (le) (rate(metric_bucket[5m])))", "quantile from a histogram"},
	{"sum", "sum by (job) (metric)", "sum across series, optionally grouped"},
	{"avg", "avg by (job) (metric)", "average across series"},
	{"min", "min by (job) (metric)", "minimum across series"},
	{"max", "max by (job) (metric)", "maximum across series"},
	{"count", "count by (job) (metric)", "number of series"},
	{"group", "group by (job) (metric)", "1 per group"},
	{"stddev", "stddev by (job) (metric)", "standard deviation across series"},
	{"stdvar", "stdvar by (job) (metric)", "variance across series"},
	{"quantile", "quantile(0.9, metric)", "quantile across series"},
	{"topk", "topk(5, metric)", "largest k series"},
	{"bottomk", "bottomk(5, metric)", "smallest k series"},
	{"count_values", "count_values(\"value\", metric)", "series count per distinct value"},
	{"absent", "absent(metric{job=\"x\"})", "1 when no series match"},
	{"abs", "abs(metric)", "absolute value"},
	{"ceil", "ceil(metric)", "round up"},
	{"floor", "floor(metric)", "round down"},
	{"round", "round(metric, 0.5)", "round to the nearest multiple"},
	{"clamp", "clamp(metric, 0, 100)", "limit values to a range"},
	{"clamp_min", "clamp_min(metric, 0)", "lower bound"},
	{"clamp_max", "clamp_max(metric, 100)", "upper bound"},
	{"label_replace", "label_replace(metric, \"dst\", \"$1\", \"src\", \"(.*)\")", "write a label from a regex over another"},
	{"label_join", "label_join(metric, \"dst\", \",\", \"a\", \"b\")", "join labels into a new label"},
	{"sort", "sort(metric)", "sort ascending"},
	{"sort_desc", "sort_desc(metric)", "sort descending"},
	{"time", "time()", "evaluation time in seconds"},
	{"timestamp", "timestamp(metric)", "timestamp of each sample"},
	{"vector", "vector(1)", "scalar as a vector"},
	{"scalar", "scalar(metric)", "single-series vector as a scalar"},
}

var lokiFunctions = []entry{
	{"count_over_time", `count_over_time({app="api"}[5m])`, "log lines per stream over the range"},
	{"rate", `rate({app="api"}[1m])`, "log lines per second"},
	{"bytes_over_time", `bytes_over_time({app="api"}[5m])`, "bytes of log lines over the range"},
	{"bytes_rate", `bytes_rate({app="api"}[1m])`, "bytes per second"},
	{"absent_over_time", `absent_over_time({app="api"}[5m])`, "1 when the stream has no lines"},
	{"sum_over_time", `sum_over_time({app="api"} | json | unwrap ms [5m])`, "sum of an unwrapped label"},
	{"avg_over_time", `avg_over_time({app="api"} | json | unwrap ms [5m])`, "average of an unwrapped label"},
	{"min_over_time", `min_over_time({app="api"} | json | unwrap ms [5m])`, "minimum of an unwrapped label"},
	{"max_over_time", `max_over_time({app="api"} | json | unwrap ms [5m])`, "maximum of an unwrapped label"},
	{"first_over_time", `first_over_time({app="api"} | json | unwrap ms [5m])`, "first unwrapped value"},
	{"last_over_time", `last_over_time({app="api"} | json | unwrap ms [5m])`, "last unwrapped value"},
	{"quantile_over_time", `quantile_over_time(0.95, {app="api"} | json | unwrap ms [5m])`, "quantile of an unwrapped label"},
	{"sum", `sum by (app) (count_over_time({env="dev"}[5m]))`, "sum across streams"},
	{"avg", `avg by (app) (rate({env="dev"}[1m]))`, "average across streams"},
	{"min", `min by (app) (rate({env="dev"}[1m]))`, "minimum across streams"},
	{"max", `max by (app) (rate({env="dev"}[1m]))`, "maximum across streams"},
	{"count", `count by (app) (rate({env="dev"}[1m]))`, "number of streams"},
	{"topk", `topk(3, sum by (app) (rate({env="dev"}[1m])))`, "largest k"},
	{"bottomk", `bottomk(3, sum by (app) (rate({env="dev"}[1m])))`, "smallest k"},
	{"stddev", `stddev by (app) (rate({env="dev"}[1m]))`, "standard deviation"},
	{"sort", `sort(sum by (app) (rate({env="dev"}[1m])))`, "sort ascending"},
	{"sort_desc", `sort_desc(sum by (app) (rate({env="dev"}[1m])))`, "sort descending"},
}

var lokiStages = []entry{
	{"|=", `{app="api"} |= "error"`, "keep lines containing the text"},
	{"!=", `{app="api"} != "health"`, "drop lines containing the text"},
	{"|~", `{app="api"} |~ "timeout|refused"`, "keep lines matching the regex"},
	{"!~", `{app="api"} !~ "debug"`, "drop lines matching the regex"},
	{"json", `{app="api"} | json`, "parse JSON lines into labels"},
	{"logfmt", `{app="api"} | logfmt`, "parse key=value lines into labels"},
	{"pattern", `{app="web"} | pattern "<method> <path> <status> <_>"`, "extract labels with a pattern"},
	{"regexp", `{app="web"} | regexp "(?P<status>\\d{3})"`, "extract labels with named groups"},
	{"label filter", `{app="api"} | json | ms > 100`, "filter on an extracted label"},
	{"line_format", `{app="api"} | json | line_format "{{.msg}}"`, "rewrite the line from labels"},
	{"label_format", `{app="api"} | label_format svc=app`, "rename or template labels"},
	{"unwrap", `{app="api"} | json | unwrap ms`, "use a label value for range aggregations"},
	{"drop / keep", `{app="api"} | json | drop env`, "remove or keep labels"},
}

func entryResult(title string, entries []entry) Result {
	r := Result{Title: title, Columns: []string{"name", "usage", "what it does"}, Wide: true}
	for _, e := range entries {
		r.Rows = append(r.Rows, []any{e.name, e.usage, e.desc})
	}
	r.Message = fmt.Sprintf("%d", len(r.Rows))
	return r
}

func commandResult(title string, rows [][2]string) Result {
	r := Result{Title: title, Columns: []string{"command", "what it does"}, Wide: true}
	for _, row := range rows {
		r.Rows = append(r.Rows, []any{row[0], row[1]})
	}
	r.Message = fmt.Sprintf("%d commands", len(rows))
	return r
}

func lookupFunction(entries []entry, input string) (entry, bool) {
	word := strings.ToLower(strings.TrimSpace(input))
	for _, e := range entries {
		if e.name == word {
			return e, true
		}
	}
	return entry{}, false
}

func functionHint(title string, e entry) []Result {
	return []Result{{
		Title:   title,
		Columns: []string{"function", "usage", "what it does"},
		Wide:    true,
		Rows:    [][]any{{e.name, e.usage, e.desc}},
		Message: "a function needs arguments · run all to list everything",
	}}
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func previewValues(values []string, limit int) string {
	if len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return strings.Join(values[:limit], ", ") + fmt.Sprintf(" … +%d", len(values)-limit)
}

func pickMetric(names []string, preferred ...string) string {
	for _, p := range preferred {
		for _, n := range names {
			if n == p {
				return n
			}
		}
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "deprecated") && !strings.HasPrefix(n, "go_godebug") {
			return n
		}
	}
	return ""
}
