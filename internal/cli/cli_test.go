package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/diegopacheco/dev-cli/internal/ui"
)

func TestParseModes(t *testing.T) {
	cases := []struct {
		args          []string
		mode, dialect string
		query         string
	}{
		{[]string{"-sql", "--postgres", "select", "1"}, "sql", "postgres", "select 1"},
		{[]string{"--sqlite", "select 1"}, "sql", "sqlite", "select 1"},
		{[]string{"--sqllite", "select 1"}, "sql", "sqlite", "select 1"},
		{[]string{"-cql", "select * from t"}, "cassandra", "", "select * from t"},
		{[]string{"-redis", "ZRANGE", "k", "0", "-1"}, "redis", "", "ZRANGE k 0 -1"},
		{[]string{"--json", "-prometheus", "up"}, "prometheus", "", "up"},
		{[]string{"-threads", "123"}, "threads", "", "123"},
		{[]string{}, "", "", ""},
	}
	for _, c := range cases {
		o, err := Parse(c.args)
		if err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if o.Mode != c.mode || o.Dialect != c.dialect || o.Query != c.query {
			t.Errorf("%v: got mode=%q dialect=%q query=%q", c.args, o.Mode, o.Dialect, o.Query)
		}
	}
}

func TestParseRejectsAmbiguousInvocations(t *testing.T) {
	for _, args := range [][]string{
		{"-sql", "select 1"},
		{"--mysql", "--postgres", "x"},
		{"-redis", "-loki", "x"},
		{"--sqlite", "-redis", "x"},
		{"select", "1"},
		{"-nope"},
	} {
		if _, err := Parse(args); err == nil {
			t.Errorf("%v must fail instead of guessing", args)
		}
	}
	if o, err := Parse([]string{"--help"}); err != nil || !o.Help {
		t.Fatal("--help must not be an error")
	}
}

func TestColorizeTranslatesTviewTags(t *testing.T) {
	in := "[#ff0000::b]x[-::-] [#00ff00]y[-] [data[]"
	if got := Colorize(in, false); got != "x y [data]" {
		t.Fatalf("plain output must drop tags and unescape data, got %q", got)
	}
	got := Colorize(in, true)
	if !strings.Contains(got, "\x1b[0;1;38;2;255;0;0mx") || !strings.Contains(got, "\x1b[0;38;2;0;255;0my") || !strings.HasSuffix(got, "[data]\x1b[0m") {
		t.Fatalf("got %q", got)
	}
}

func stdio(in string, tty bool) (IO, *bytes.Buffer, *bytes.Buffer) {
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	return IO{In: strings.NewReader(in), Out: out, Err: errb, OutTTY: tty, ErrTTY: tty}, out, errb
}

func TestMainPipesPlainJSONThatParses(t *testing.T) {
	o, _ := Parse([]string{"-json", "--sqlite", "-target", ":memory:"})
	io, out, errb := stdio("select 1 as n, '{\"a\":[1,2]}' as doc;", false)
	if code := Main(o, DefaultTargets(), io); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("piped -json output must be valid JSON for jq, got %q: %v", out.String(), err)
	}
	if rows[0]["n"] != float64(1) || rows[0]["doc"].(map[string]any)["a"] == nil {
		t.Fatalf("got %v", rows)
	}
	if strings.Contains(out.String(), "\x1b") || errb.Len() != 0 {
		t.Fatal("no colors and no banner when not on a terminal")
	}
}

func TestMainOnTerminalShowsBannerHeaderAndColors(t *testing.T) {
	o, _ := Parse([]string{"--sqlite", "-target", ":memory:", "select 42 as answer"})
	io, out, errb := stdio("", true)
	if code := Main(o, DefaultTargets(), io); code != 0 {
		t.Fatal(errb.String())
	}
	if !strings.Contains(regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(errb.String(), ""), "██████╗") {
		t.Fatal("the ASCII banner goes to stderr on a terminal so stdout stays clean")
	}
	if !strings.Contains(out.String(), "▶") || !strings.Contains(out.String(), "\x1b[") || !strings.Contains(out.String(), "42") {
		t.Fatalf("got %q", out.String())
	}
}

func TestMainQuietAndErrorsExitNonZero(t *testing.T) {
	o, _ := Parse([]string{"-q", "--sqlite", "-target", ":memory:", "select * from missing"})
	io, _, errb := stdio("", true)
	if code := Main(o, DefaultTargets(), io); code != 1 {
		t.Fatalf("a failing query must exit 1, got %d", code)
	}
	if strings.Contains(errb.String(), "██") || !strings.Contains(errb.String(), "missing") {
		t.Fatalf("-q hides the banner but never the error, got %q", errb.String())
	}
	o, _ = Parse([]string{"-redis"})
	io, _, errb = stdio("", true)
	io.InTTY = true
	if Main(o, DefaultTargets(), io) != 1 || !strings.Contains(errb.String(), "needs a query") {
		t.Fatalf("got %q", errb.String())
	}
}

func TestMainHelpHasBannerAndEveryMode(t *testing.T) {
	o, _ := Parse([]string{"--help"})
	io, out, _ := stdio("", false)
	if Main(o, DefaultTargets(), io) != 0 {
		t.Fatal("help exits 0")
	}
	for _, want := range []string{"██████╗", "-sql --postgres", "--sqlite", "-redis", "-loki", "-grafana", "-prometheus", "-cassandra", "-discover", "DEVCLI_PROMETHEUS"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help is missing %q", want)
		}
	}
}

func TestExecutePrometheusThroughTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/query" {
			w.Write([]byte(`{"data":{"resultType":"vector","result":[{"metric":{"job":"api"},"value":[1,"7"]}]}}`))
			return
		}
		w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	o, _ := Parse([]string{"-prometheus", "-target", srv.URL, "up"})
	res, err := Execute(context.Background(), o, ui.Targets{Prometheus: "http://127.0.0.1:1"})
	if err != nil || res[0].Rows[0][2] != "7" {
		t.Fatalf("-target must override the default, got %+v %v", res, err)
	}
}
