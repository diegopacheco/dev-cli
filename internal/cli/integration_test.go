//go:build integration

package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

func run(t *testing.T, args ...string) string {
	t.Helper()
	o, err := Parse(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results, err := Execute(ctx, o, DefaultTargets())
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	var b strings.Builder
	for _, r := range results {
		b.WriteString(syntax.Pretty(r.Value))
		b.WriteString(syntax.Pretty(rowsText(r.Rows)))
		b.WriteString(r.Text)
	}
	return b.String()
}

func rowsText(rows [][]any) []any {
	out := []any{}
	for _, r := range rows {
		out = append(out, r)
	}
	return out
}

func TestIntegrationEveryNonInteractiveMode(t *testing.T) {
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"-sql", "--postgres", "select count(*) from events"}, "200"},
		{[]string{"-sql", "--mysql", "select count(*) from events"}, "200"},
		{[]string{"--sqlite", "select count(*) from metrics where name = 'latency_ms'"}, "100"},
		{[]string{"-cql", "select name from devcli.users where id = 1"}, "Ana Ribeiro"},
		{[]string{"-redis", "XLEN stream:events"}, "3"},
		{[]string{"-loki", `{app="worker"}`}, "backup"},
		{[]string{"-grafana", "datasources"}, "prometheus"},
		{[]string{"-prometheus", "up"}, "loki:3100"},
		{[]string{"-prometheus", "all"}, "prometheus_http_request_duration_seconds_bucket"},
		{[]string{"-loki", "all"}, `{app=\"api\"} | json`},
		{[]string{"-grafana", "all"}, "dashboard devcli-logs"},
		{[]string{"-threads"}, "sample/java25/src/DevcliJvm.java"},
		{[]string{"-discover"}, "devcli-cassandra"},
		{[]string{"-threads"}, "Clojure"},
		{[]string{"-containers"}, "devcli-grafana"},
	}
	for _, c := range checks {
		if out := run(t, c.args...); !strings.Contains(out, c.want) {
			t.Errorf("%v: expected %q in output", c.args, c.want)
		}
	}
}
