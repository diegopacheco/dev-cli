package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

func TestSemicolonInsideQuotesDoesNotSplitStatement(t *testing.T) {
	got := SplitStatements("insert into t values ('a;b'); select 1; -- tail;\n")
	if len(got) != 2 || got[0] != "insert into t values ('a;b')" || got[1] != "select 1" {
		t.Fatalf("a ; inside data would run half a statement, got %q", got)
	}
}

func TestMySQLURLBecomesDriverDSN(t *testing.T) {
	dsn, err := mysqlDSN("mysql://root:pw@127.0.0.1:3306/devcli")
	if err != nil || dsn != "root:pw@tcp(127.0.0.1:3306)/devcli?parseTime=true" {
		t.Fatalf("got %q %v", dsn, err)
	}
}

func TestEnterRunsSQLOnlyAfterSemicolon(t *testing.T) {
	s := NewSQLite(":memory:")
	if s.RunsOnEnter("select *\nfrom t") || !s.RunsOnEnter("select 1;  ") {
		t.Fatal("multi-line SQL needs Enter for newlines until the statement is terminated")
	}
}

func TestSQLiteRowsAffectedAndCompletionWords(t *testing.T) {
	ctx := context.Background()
	s := NewSQLite(":memory:")
	if err := s.Connect(ctx, ":memory:"); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, err := s.Execute(ctx, "create table hosts (id integer, meta text); insert into hosts values (1, '{\"cpu\":8}'), (2, null); select id, meta from hosts order by id;")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 || res[1].Message != "2 rows affected" {
		t.Fatalf("insert must report affected rows: %+v", res)
	}
	sel := res[2]
	if len(sel.Rows) != 2 || sel.Columns[1] != "meta" || syntax.Scalar(sel.Rows[1][1]) != "NULL" {
		t.Fatalf("select rows wrong: %+v", sel)
	}
	words := s.Words(ctx)
	if !slices.Contains(words, "hosts") || !slices.Contains(words, "meta") {
		t.Fatalf("completion must know live tables and columns, got %v", words)
	}
}

func TestSQLErrorNamesTheFailingStatement(t *testing.T) {
	ctx := context.Background()
	s := NewSQLite(":memory:")
	s.Connect(ctx, ":memory:")
	defer s.Close()
	_, err := s.Execute(ctx, "select 1; select * from missing;")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("got %v", err)
	}
}

func TestTypedTurnsNumericBytesIntoNumbers(t *testing.T) {
	if _, ok := typed("DECIMAL", []byte("12.50")).(json.Number); !ok {
		t.Fatal("MySQL decimals arrive as bytes and must render as JSON numbers")
	}
	if typed("VARCHAR", []byte("12")) != "12" {
		t.Fatal("text stays text")
	}
}

func TestRESPEncodingIsBinarySafe(t *testing.T) {
	got := string(EncodeCommand([]string{"SET", "k", "a b"}))
	if got != "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$3\r\na b\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRESPParsesEveryReplyType(t *testing.T) {
	raw := "*6\r\n+OK\r\n:42\r\n$5\r\nhello\r\n$-1\r\n*1\r\n$2\r\nhi\r\n-ERR inner\r\n"
	v, err := ReadReply(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	arr := v.([]any)
	if arr[0] != "OK" || arr[1] != json.Number("42") || arr[2] != "hello" || arr[3] != nil || arr[4].([]any)[0] != "hi" || arr[5] != "(error) ERR inner" {
		t.Fatalf("got %#v", arr)
	}
	if _, err := ReadReply(bufio.NewReader(strings.NewReader("-WRONGTYPE bad\r\n"))); err == nil || err.Error() != "WRONGTYPE bad" {
		t.Fatalf("server errors must surface verbatim, got %v", err)
	}
}

func TestSplitArgsHonorsQuotes(t *testing.T) {
	args, err := SplitArgs(`SET user:1 "{\"name\": \"Ana\"}" 'x y'`)
	if err != nil || len(args) != 4 || args[2] != `{"name": "Ana"}` || args[3] != "x y" {
		t.Fatalf("got %q %v", args, err)
	}
	if _, err := SplitArgs(`GET "open`); err == nil {
		t.Fatal("unterminated quote must not send a truncated command")
	}
}

func fakeRedis(t *testing.T, handle func(args []string) string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					v, err := ReadReply(r)
					if err != nil {
						if err != io.EOF {
							return
						}
						return
					}
					var args []string
					for _, a := range v.([]any) {
						args = append(args, a.(string))
					}
					c.Write([]byte(handle(args)))
				}
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestRedisBackendShapesHashesAndCompletesKeys(t *testing.T) {
	addr := fakeRedis(t, func(args []string) string {
		switch strings.ToUpper(args[0]) {
		case "PING":
			return "+PONG\r\n"
		case "HGETALL":
			return "*4\r\n$4\r\nuser\r\n$1\r\n1\r\n$2\r\nip\r\n$8\r\n10.0.0.7\r\n"
		case "SCAN":
			return "*2\r\n$1\r\n0\r\n*2\r\n$6\r\nuser:1\r\n$6\r\nuser:2\r\n"
		}
		return "-ERR unknown\r\n"
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := NewRedis("redis://" + addr)
	if err := r.Connect(ctx, "redis://"+addr+"/0"); err != nil {
		t.Fatal(err)
	}
	res, err := r.Execute(ctx, "HGETALL session:1")
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := res[0].Value.(syntax.Object)
	if !ok || obj[1].Key != "ip" {
		t.Fatalf("HGETALL must render as an object, got %#v", res[0].Value)
	}
	if words := r.Words(ctx); !slices.Equal(words, []string{"user:1", "user:2"}) {
		t.Fatalf("got %v", words)
	}
	if _, err := r.Execute(ctx, "BOGUS"); err == nil || !strings.Contains(err.Error(), "ERR unknown") {
		t.Fatalf("got %v", err)
	}
}

func TestLokiQueryWindowAndStreamShaping(t *testing.T) {
	now := time.Unix(1700000000, 0)
	var gotQuery, gotStart string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.Write([]byte(`{"status":"success","data":["app","level"]}`))
		case "/loki/api/v1/query_range":
			gotQuery, gotStart = r.URL.Query().Get("query"), r.URL.Query().Get("start")
			w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[
				{"stream":{"app":"api"},"values":[["1700000000000000000","{\"msg\":\"old\"}"]]},
				{"stream":{"app":"web"},"values":[["1700000001000000000","newer"]]}]}}`))
		default:
			http.Error(w, "parse error at line 1", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	l := NewLoki(srv.URL)
	l.Now = func() time.Time { return now }
	if err := l.Connect(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Execute(ctx, ":range 30m"); err != nil {
		t.Fatal(err)
	}
	res, err := l.Execute(ctx, `{app=~".+"} |= "x"`)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != `{app=~".+"} |= "x"` || gotStart != "1699998200000000000" {
		t.Fatalf("query %q start %q: :range must move the window start", gotQuery, gotStart)
	}
	logs := res[0].Logs
	if len(logs) != 2 || logs[0].Line != "newer" || logs[1].Labels[0].Value != "api" {
		t.Fatalf("lines from all streams must be merged newest first, got %+v", logs)
	}
	if _, err := l.Execute(ctx, "values"); err == nil {
		t.Fatal("values without a label must explain usage")
	}
}

func TestLokiErrorsSurfaceServerMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/loki/api/v1/labels" {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		http.Error(w, "parse error : syntax error", http.StatusBadRequest)
	}))
	defer srv.Close()
	l := NewLoki(srv.URL)
	l.Connect(context.Background(), srv.URL)
	_, err := l.Execute(context.Background(), "{bad")
	if err == nil || !strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("the LogQL parse error is what the user needs to fix the query, got %v", err)
	}
}

func TestGrafanaUsesBasicAuthAndShapesFrames(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "devcli" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/search":
			w.Write([]byte(`[{"type":"dash-db","uid":"devcli-logs","title":"devcli logs"}]`))
		case "/api/datasources":
			w.Write([]byte(`[{"uid":"loki","name":"Loki","type":"loki"}]`))
		case "/api/ds/query":
			b, _ := io.ReadAll(r.Body)
			body = string(b)
			w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"Time"},{"name":"Line"}]},"data":{"values":[[1,2],["a","b"]]}}]}}}`))
		}
	}))
	defer srv.Close()
	target := strings.Replace(srv.URL, "http://", "http://admin:devcli@", 1)
	ctx := context.Background()
	g := NewGrafana(target, "")
	if err := g.Connect(ctx, target); err != nil {
		t.Fatal(err)
	}
	res, err := g.Execute(ctx, `query loki {app="api"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"uid":"loki"`) || !strings.Contains(body, `"expr":"{app=\"api\"}"`) {
		t.Fatalf("datasource and expression must reach /api/ds/query, got %s", body)
	}
	r := res[0]
	if len(r.Rows) != 2 || r.Columns[1] != "Line" || r.Rows[1][1] != "b" {
		t.Fatalf("columnar frames must become rows, got %+v", r)
	}
	words := g.Words(ctx)
	if !slices.Contains(words, "devcli-logs") || !slices.Contains(words, "loki") {
		t.Fatalf("dashboard and datasource uids must complete, got %v", words)
	}
	if _, err := g.Execute(ctx, "nonsense"); err == nil || !strings.Contains(err.Error(), "commands:") {
		t.Fatalf("unknown commands must list what is available, got %v", err)
	}
}

func TestPrometheusInstantRangeAndTargets(t *testing.T) {
	now := time.Unix(1700000000, 0)
	var gotPath, gotStep string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.Write([]byte(`{"status":"success","data":{"version":"3.0.0"}}`))
		case "/api/v1/query", "/api/v1/query_range":
			gotPath, gotStep = r.URL.Path, r.URL.Query().Get("step")
			w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","job":"loki"},"value":[1700000000,"1"]}]}}`))
		case "/api/v1/targets":
			w.Write([]byte(`{"data":{"activeTargets":[{"labels":{"job":"loki","instance":"loki:3100"},"health":"up"}]}}`))
		case "/api/v1/label/__name__/values":
			w.Write([]byte(`{"data":["up","go_goroutines"]}`))
		case "/api/v1/labels":
			w.Write([]byte(`{"data":["job","instance"]}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	p := NewPrometheus(srv.URL)
	p.Now = func() time.Time { return now }
	if err := p.Connect(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	res, err := p.Execute(ctx, "up\n:range 10m\nup\ntargets")
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Rows[0][0] != `{__name__="up", job="loki"}` || res[0].Rows[0][2] != "1" {
		t.Fatalf("instant vector must list series and value, got %+v", res[0].Rows)
	}
	if gotPath != "/api/v1/query_range" || gotStep != "5" {
		t.Fatalf(":range must switch to query_range with a step of range/120, got %s step=%s", gotPath, gotStep)
	}
	if res[3].Rows[0][2] != "up" {
		t.Fatalf("targets must show health, got %+v", res[3].Rows)
	}
	words := p.Words(ctx)
	if !slices.Contains(words, "go_goroutines") || !slices.Contains(words, "instance") {
		t.Fatalf("metric and label names must complete, got %v", words)
	}
}

func titles(results []Result) []string {
	var out []string
	for _, r := range results {
		out = append(out, r.Title)
	}
	return out
}

func find(results []Result, title string) Result {
	for _, r := range results {
		if r.Title == title {
			return r
		}
	}
	return Result{}
}

func column(r Result, i int) []string {
	var out []string
	for _, row := range r.Rows {
		out = append(out, syntax.Scalar(row[i]))
	}
	return out
}

func TestPrometheusAllListsEverythingAndReadyQueriesUseRealMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.Write([]byte(`{"data":{}}`))
		case "/api/v1/metadata":
			w.Write([]byte(`{"data":{"deprecated_flags_inuse_total":[{"type":"counter","help":"old"}],"http_requests_total":[{"type":"counter","help":"requests"}],"queue_depth":[{"type":"gauge","help":"depth"}]}}`))
		case "/api/v1/labels":
			w.Write([]byte(`{"data":["__name__","job"]}`))
		case "/api/v1/label/job/values":
			w.Write([]byte(`{"data":["api","worker"]}`))
		case "/api/v1/targets":
			w.Write([]byte(`{"data":{"activeTargets":[{"labels":{"job":"api","instance":"api:80"},"health":"up"}]}}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	p := NewPrometheus(srv.URL)
	p.Connect(ctx, srv.URL)
	res, err := p.Execute(ctx, "all")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(titles(res), ",") != "commands,ready queries,metrics,labels,targets,functions" {
		t.Fatalf("got sections %v", titles(res))
	}
	ready := strings.Join(column(find(res, "ready queries"), 0), "\n")
	if !strings.Contains(ready, "rate(http_requests_total[1m])") || strings.Contains(ready, "deprecated") {
		t.Fatalf("ready queries must use a meaningful live metric, got %s", ready)
	}
	if !strings.Contains(strings.Join(column(find(res, "labels"), 1), " "), "api, worker") {
		t.Fatal("labels must preview their values")
	}
	if !find(res, "ready queries").Wide {
		t.Fatal("ready queries must not be truncated or they cannot be copied")
	}
}

func TestBareFunctionNameExplainsUsageInsteadOfQuerying(t *testing.T) {
	queried := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "query") {
			queried = true
		}
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	p := NewPrometheus(srv.URL)
	p.Connect(ctx, srv.URL)
	l := NewLoki(srv.URL)
	l.Connect(ctx, srv.URL)
	for _, b := range []Backend{p, l} {
		res, err := b.Execute(ctx, "avg_over_time")
		if err != nil || len(res) != 1 || !strings.Contains(syntax.Scalar(res[0].Rows[0][1]), "avg_over_time(") {
			t.Fatalf("%s: typing a function name must show how to call it, got %+v %v", b.Name(), res, err)
		}
	}
	if queried {
		t.Fatal("a bare function name is not a query and must not be sent to the server")
	}
}

func TestLokiAllBuildsReadyQueriesFromLiveLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.Write([]byte(`{"data":["app","level"]}`))
		case "/loki/api/v1/label/app/values":
			w.Write([]byte(`{"data":["checkout","web"]}`))
		case "/loki/api/v1/label/level/values":
			w.Write([]byte(`{"data":["error","info"]}`))
		case "/loki/api/v1/series":
			if len(r.URL.Query()["match[]"]) != 2 {
				http.Error(w, "need a matcher per label", 400)
				return
			}
			w.Write([]byte(`{"data":[{"app":"checkout","level":"error"},{"app":"web","level":"info"}]}`))
		default:
			http.Error(w, "parse error at line 1, col 1: syntax error: unexpected IDENTIFIER", 400)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	l := NewLoki(srv.URL)
	l.Connect(ctx, srv.URL)
	res, err := l.Execute(ctx, "all")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(titles(res), ",") != "commands,ready queries,labels,streams,pipeline stages,functions" {
		t.Fatalf("got %v", titles(res))
	}
	ready := column(find(res, "ready queries"), 0)
	if len(ready) == 0 || ready[0] != `{app="checkout"}` {
		t.Fatalf("the first ready query must select a real stream, got %v", ready)
	}
	if len(find(res, "streams").Rows) != 2 {
		t.Fatal("streams must be listed")
	}
	_, err = l.Execute(ctx, "checkout errors")
	if err == nil || !strings.Contains(err.Error(), "run all") || !strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("a parse error must keep Loki's message and point to all, got %v", err)
	}
}

func TestGrafanaAllListsServerObjectsAndReadyCommands(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.Write([]byte(`{"database":"ok","version":"12.0.0"}`))
		case "/api/search":
			w.Write([]byte(`[{"type":"dash-db","uid":"svc-overview","title":"Service overview"}]`))
		case "/api/datasources":
			w.Write([]byte(`[{"uid":"logs","name":"Logs","type":"loki"},{"uid":"metrics","name":"Metrics","type":"prometheus"}]`))
		case "/api/folders":
			w.Write([]byte(`[{"uid":"f1","title":"Team"}]`))
		case "/api/v1/provisioning/alert-rules":
			http.Error(w, "forbidden", 403)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	g := NewGrafana(srv.URL, "")
	g.Connect(ctx, srv.URL)
	res, err := g.Execute(ctx, "all")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(titles(res), ",") != "commands,ready commands,health,datasources,folders,dashboards,alert rules" {
		t.Fatalf("got %v", titles(res))
	}
	ready := strings.Join(column(find(res, "ready commands"), 0), "\n")
	for _, want := range []string{"query logs {service_name=", "query metrics up", "dashboard svc-overview"} {
		if !strings.Contains(ready, want) {
			t.Errorf("ready commands must be built from the server's own datasources and dashboards, missing %q in %s", want, ready)
		}
	}
	if !strings.Contains(find(res, "alert rules").Message, "unavailable") {
		t.Fatal("a section the token cannot read must say so instead of failing the whole listing")
	}
}

func TestSQLiteAllListsSchemaObjectsAndEveryReadyQueryRuns(t *testing.T) {
	ctx := context.Background()
	s := NewSQLite(":memory:")
	if err := s.Connect(ctx, ":memory:"); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err := s.Execute(ctx, `create table hosts (id integer primary key, name text);
create table metrics (id integer, host_id integer references hosts(id), value real);
create index metrics_host on metrics(host_id);
create view busy as select * from metrics where value > 1;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("create trigger touch after insert on hosts begin select 1; end"); err != nil {
		t.Fatal(err)
	}
	res, err := s.Execute(ctx, "all")
	if err != nil {
		t.Fatal(err)
	}
	want := "commands,ready queries,server,databases,tables,columns,indexes,foreign keys,triggers,pragmas,functions"
	if strings.Join(titles(res), ",") != want {
		t.Fatalf("got sections %v", titles(res))
	}
	for _, r := range res {
		if strings.HasPrefix(r.Message, "unavailable") {
			t.Fatalf("%s must load on SQLite: %s", r.Title, r.Message)
		}
	}
	checks := map[string]string{"tables": "busy", "indexes": "metrics_host", "triggers": "touch", "foreign keys": "hosts", "pragmas": "journal_mode"}
	for title, name := range checks {
		r := find(res, title)
		found := false
		for i := range r.Columns {
			found = found || slices.Contains(column(r, i), name)
		}
		if !found {
			t.Errorf("%s must list %s, got %v", title, name, r.Rows)
		}
	}
	ready := column(find(res, "ready queries"), 0)
	if !slices.Contains(ready, "SELECT * FROM hosts LIMIT 10;") || !slices.Contains(ready, "PRAGMA table_info(metrics);") {
		t.Fatalf("ready queries must come from the live tables, got %v", ready)
	}
	for _, q := range ready {
		if _, err := s.Execute(ctx, q); err != nil {
			t.Errorf("a ready query must run as loaded into the editor: %s: %v", q, err)
		}
	}
}

func TestAllRunsOnEnterWithoutASemicolon(t *testing.T) {
	for _, b := range []Backend{NewSQLite(""), NewPostgres(""), NewMySQL(""), NewCassandra("")} {
		if !b.RunsOnEnter("all") || !b.RunsOnEnter(" ALL;\n") {
			t.Errorf("%s: the connected hint says type all and press Enter, so Enter must run it", b.Name())
		}
		if b.RunsOnEnter("select all") {
			t.Errorf("%s: statements that only contain the word all still need a ;", b.Name())
		}
	}
}

func TestQuoteNameKeepsPlainTablesReadable(t *testing.T) {
	if quoteName("users", `"`) != "users" || quoteName("sales.orders", `"`) != "sales.orders" || quoteName("Order Items", "`") != "`Order Items`" {
		t.Fatal("only names that would not parse get quoted")
	}
}

func TestChartIntervalFollowsTheDashboardRange(t *testing.T) {
	if intervalMs("now-1h", "now") != 12000 || intervalMs("now-7d", "now-6d") != 288000 {
		t.Fatal("the step must give about 300 points so a chart is neither empty nor one point per second")
	}
	if intervalMs("2026-01-01", "now") != 15000 {
		t.Fatal("absolute ranges fall back to 15s")
	}
}

func frame(name string, labels, config string, times, values string) string {
	return `{"schema":{"fields":[{"name":"Time","type":"time","config":{"interval":15000}},{"name":"Value","type":"number","labels":` + labels + `,"config":` + config + `}]},"data":{"values":[` + times + `,` + values + `]}}`
}

func TestGrafanaChartRunsEveryPanelQueryAndShapesByPanelType(t *testing.T) {
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search":
			w.Write([]byte(`[]`))
		case "/api/dashboards/uid/svc":
			w.Write([]byte(`{"dashboard":{"title":"svc","time":{"from":"now-1h","to":"now"},"panels":[
{"id":1,"type":"stat","title":"Targets up","datasource":{"uid":"prom"},"targets":[{"refId":"A","expr":"sum(up)","legendFormat":"up"},{"refId":"B","expr":"hidden","hide":true}]},
{"id":2,"type":"text","title":"Notes"},
{"id":3,"type":"row","title":"details","panels":[
  {"id":4,"type":"timeseries","title":"Latency","datasource":{"uid":"prom"},"fieldConfig":{"defaults":{"unit":"s"}},"targets":[{"refId":"A","datasource":{"uid":"other"},"expr":"latency"}]},
  {"id":5,"type":"logs","title":"API logs","datasource":{"uid":"loki"},"targets":[{"refId":"A","expr":"{app=\"api\"}"}]}
]}]}}`))
		case "/api/ds/query":
			body, _ := io.ReadAll(r.Body)
			sent = append(sent, string(body))
			switch {
			case strings.Contains(string(body), "sum(up)"):
				w.Write([]byte(`{"results":{"A":{"frames":[` + frame("", `{}`, `{"displayNameFromDS":"up"}`, `[1000,16000,31000]`, `[2,3,3]`) + `]}}}`))
			case strings.Contains(string(body), "latency"):
				w.Write([]byte(`{"results":{"A":{"frames":[` + frame("", `{"job":"api"}`, `{}`, `[1000,16000]`, `[0.2,null]`) + `,` + frame("", `{"job":"db"}`, `{}`, `[1000,16000]`, `[0.4,0.5]`) + `]}}}`))
			default:
				w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"labels","type":"other"},{"name":"Time","type":"time"},{"name":"Line","type":"string"}]},"data":{"values":[[{"app":"api","level":"error"},{"app":"api","level":"info"}],[1000,5000],["old","new"]]}}]}}}`))
			}
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	g := NewGrafana(srv.URL, "")
	if err := g.Connect(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	res, err := g.Execute(ctx, "chart svc")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(titles(res), ",") != "Targets up,Notes,Latency,API logs" {
		t.Fatalf("panels inside collapsed rows must be drawn too, got %v", titles(res))
	}
	stat := res[0].Chart
	if stat == nil || stat.Kind != "stat" || stat.Series[0].Name != "up" || stat.Series[0].Values[2] != 3 {
		t.Fatalf("a stat panel must keep its kind and the legend name from Grafana, got %+v", stat)
	}
	if strings.Contains(sent[0], "hidden") || !strings.Contains(sent[0], `"legendFormat":"up"`) || !strings.Contains(sent[0], `"intervalMs":12000`) || !strings.Contains(sent[0], `"uid":"prom"`) {
		t.Fatalf("the query must be the panel's own target with the panel datasource and a sane step, sent %s", sent[0])
	}
	if res[1].Chart != nil || !strings.Contains(res[1].Message, "no queries") {
		t.Fatalf("a panel without queries must say so, got %+v", res[1])
	}
	latency := res[2].Chart
	if latency == nil || latency.Unit != "s" || len(latency.Series) != 2 || latency.Series[0].Name != "job=api" || !math.IsNaN(latency.Series[0].Values[1]) {
		t.Fatalf("every frame is a series, labels name it and null stays a gap, got %+v", latency)
	}
	if !strings.Contains(sent[1], `"uid":"other"`) {
		t.Fatalf("a target datasource overrides the panel datasource, sent %s", sent[1])
	}
	logs := res[3].Logs
	if res[3].Chart != nil || len(logs) != 2 || logs[0].Line != "new" || syntax.Scalar(logs[1].Labels[1].Value) != "error" {
		t.Fatalf("a logs panel must show log lines newest first, got %+v", logs)
	}
	one, err := g.Execute(ctx, "chart svc latency")
	if err != nil || len(one) != 1 || one[0].Title != "Latency" {
		t.Fatalf("chart with a title fragment must draw only that panel, got %v %v", titles(one), err)
	}
	if _, err := g.Execute(ctx, "chart svc nothing"); err == nil {
		t.Fatal("a panel filter matching nothing must fail loud")
	}
}
