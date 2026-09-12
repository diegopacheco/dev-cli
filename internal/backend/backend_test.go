package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
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
