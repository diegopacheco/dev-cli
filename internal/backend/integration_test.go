//go:build integration

package backend

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

func target(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func connect(t *testing.T, b Backend, tgt string) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if err := b.Connect(ctx, tgt); err != nil {
		t.Fatalf("%s: %v (is scripts/start-all.sh running?)", b.Name(), err)
	}
	t.Cleanup(func() { b.Close() })
	return ctx
}

func assertSeededUsers(t *testing.T, b Backend, ctx context.Context, query string) {
	t.Helper()
	res, err := b.Execute(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	r := res[len(res)-1]
	if len(r.Rows) < 3 || !slices.Contains(r.Columns, "profile") {
		t.Fatalf("%s: seeded users missing: %+v", b.Name(), r)
	}
	if !strings.Contains(syntax.Pretty(r.Rows[0][slices.Index(r.Columns, "profile")]), `"lang"`) {
		t.Fatalf("%s: JSON profile column must pretty print as JSON", b.Name())
	}
	words := b.Words(ctx)
	if !slices.Contains(words, "users") || !slices.Contains(words, "profile") {
		t.Fatalf("%s: completion must include live tables and columns, got %v", b.Name(), words)
	}
}

func TestIntegrationMySQL(t *testing.T) {
	b := NewMySQL("")
	ctx := connect(t, b, target("DEVCLI_MYSQL", "mysql://root:devcli@127.0.0.1:3306/devcli"))
	assertSeededUsers(t, b, ctx, "SELECT id, name, profile FROM users ORDER BY id;")
}

func TestIntegrationPostgres(t *testing.T) {
	b := NewPostgres("")
	ctx := connect(t, b, target("DEVCLI_POSTGRES", "postgres://postgres:devcli@127.0.0.1:5432/devcli?sslmode=disable"))
	assertSeededUsers(t, b, ctx, "SELECT id, name, profile FROM users ORDER BY id;")
}

func TestIntegrationSQLite(t *testing.T) {
	path := target("DEVCLI_SQLITE", "../../.run/devcli.db")
	b := NewSQLite("")
	ctx := connect(t, b, path)
	res, err := b.Execute(ctx, "SELECT name, meta FROM hosts ORDER BY id;")
	if err != nil || len(res[0].Rows) != 3 {
		t.Fatalf("seeded hosts missing: %+v %v", res, err)
	}
}

func TestIntegrationCassandra(t *testing.T) {
	b := NewCassandra("")
	ctx := connect(t, b, target("DEVCLI_CASSANDRA", "127.0.0.1:9042/devcli"))
	assertSeededUsers(t, b, ctx, "SELECT id, name, tags, profile FROM users;")
}

func TestIntegrationRedis(t *testing.T) {
	b := NewRedis("")
	ctx := connect(t, b, target("DEVCLI_REDIS", "redis://127.0.0.1:6380/0"))
	res, err := b.Execute(ctx, "GET user:1\nHGETALL session:9f2c")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(syntax.Pretty(res[0].Value), `"lang"`) {
		t.Fatal("a JSON string value must pretty print as JSON")
	}
	if _, ok := res[1].Value.(syntax.Object); !ok {
		t.Fatalf("HGETALL must be an object, got %#v", res[1].Value)
	}
	if words := b.Words(ctx); !slices.Contains(words, "user:1") {
		t.Fatalf("keys must complete, got %v", words)
	}
}

func TestIntegrationLoki(t *testing.T) {
	b := NewLoki("")
	ctx := connect(t, b, target("DEVCLI_LOKI", "http://127.0.0.1:3100"))
	res, err := b.Execute(ctx, `{app="api"} | json`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res[0].Logs) == 0 {
		t.Fatal("seeded api logs missing; run scripts/start-all.sh")
	}
	if words := b.Words(ctx); !slices.Contains(words, "app") || !slices.Contains(words, "worker") {
		t.Fatalf("label names and values must complete, got %v", words)
	}
}

func TestIntegrationGrafana(t *testing.T) {
	b := NewGrafana("", "")
	ctx := connect(t, b, target("DEVCLI_GRAFANA", "http://admin:devcli@127.0.0.1:3000"))
	res, err := b.Execute(ctx, "dashboard devcli-logs\ndatasources")
	if err != nil {
		t.Fatal(err)
	}
	if len(res[0].Rows) != 2 || len(res[1].Rows) == 0 {
		t.Fatalf("provisioned dashboard panels and Loki datasource expected: %+v", res)
	}
	q, err := b.Execute(ctx, `query loki {app="api"}`)
	if err != nil || len(q[0].Rows) == 0 {
		t.Fatalf("a Loki query through Grafana must return rows: %+v %v", q, err)
	}
}

func TestIntegrationPrometheus(t *testing.T) {
	b := NewPrometheus("")
	ctx := connect(t, b, target("DEVCLI_PROMETHEUS", "http://127.0.0.1:9090"))
	res, err := b.Execute(ctx, "up\ntargets")
	if err != nil {
		t.Fatal(err)
	}
	if len(res[0].Rows) < 3 || len(res[1].Rows) < 3 {
		t.Fatalf("prometheus scrapes itself, loki and grafana: %+v", res)
	}
	if words := b.Words(ctx); !slices.Contains(words, "up") {
		t.Fatalf("metric names must complete, got %d words", len(words))
	}
}
