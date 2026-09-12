package syntax

import (
	"strings"
	"testing"
)

func kinds(lang Language, src string) map[string]Kind {
	line := []rune(src)
	toks, _ := Lex(lang, line, false)
	out := map[string]Kind{}
	for _, t := range toks {
		out[string(line[t.Start:t.End])] = t.Kind
	}
	return out
}

func TestKeywordInsideStringOrCommentIsNotHighlightedAsKeyword(t *testing.T) {
	k := kinds(SQL, `select 'from users' -- where id`)
	if k["select"] != Keyword {
		t.Fatalf("select should be keyword, got %v", k["select"])
	}
	if k["'from users'"] != String {
		t.Fatalf("quoted text must stay a string so a user sees it is data, got %v", k)
	}
	if k["-- where id"] != Comment {
		t.Fatalf("comment must swallow the rest of the line, got %v", k)
	}
}

func TestLogQLPipeOperatorsAreSingleTokens(t *testing.T) {
	k := kinds(LogQL, `{app="api"} |= "error" | json`)
	if k["|="] != Operator {
		t.Fatalf("|= must be one operator token, got %v", k)
	}
	if k["app"] != Label {
		t.Fatalf("label names inside a selector should be colored as labels, got %v", k)
	}
	if k["json"] != Keyword {
		t.Fatalf("json parser stage should be keyword, got %v", k)
	}
}

func TestRedisKeysWithColonsStayOneWord(t *testing.T) {
	k := kinds(Redis, "ZRANGE user:1 0 -1")
	if k["user:1"] != Plain || k["ZRANGE"] != Keyword || k["1"] != Number {
		t.Fatalf("a key like user:1 is one identifier, not a key plus a bind variable, got %v", k)
	}
	if k := kinds(SQL, "where id = :id"); k[":id"] != Variable {
		t.Fatalf("SQL bind variables still highlight, got %v", k)
	}
}

func TestBlockCommentSpansLines(t *testing.T) {
	_, open := Lex(SQL, []rune("select /* start"), false)
	if !open {
		t.Fatal("unterminated block comment must carry to the next line")
	}
	toks, open := Lex(SQL, []rune("still comment */ from"), true)
	if open || toks[0].Kind != Comment || toks[len(toks)-1].Kind != Keyword {
		t.Fatalf("comment should close and FROM should be a keyword again: %v", toks)
	}
}

func TestCompletionPrefersPrefixOverSubstring(t *testing.T) {
	got := Complete("ord", []string{"records", "orders", "ORDER"}, 8)
	if len(got) != 3 || got[0] != "ORDER" || got[1] != "orders" || got[2] != "records" {
		t.Fatalf("prefix matches must come before substring matches, got %v", got)
	}
}

func TestCompletionSkipsExactWordAlreadyTyped(t *testing.T) {
	if got := Complete("users", []string{"users"}, 8); len(got) != 0 {
		t.Fatalf("offering the word already typed makes Tab a no-op trap, got %v", got)
	}
}

func TestKeywordCaseFollowsUser(t *testing.T) {
	if MatchCase("sel", "SELECT", SQL) != "select" {
		t.Fatal("lowercase typing must insert lowercase keyword")
	}
	if MatchCase("us", "Users", SQL) != "Users" {
		t.Fatal("identifiers keep their real case")
	}
}

func TestWordBeforeCursorIncludesQualifiedNames(t *testing.T) {
	w, start := WordBefore([]rune("select u.na"), 11)
	if w != "u.na" || start != 7 {
		t.Fatalf("got %q at %d", w, start)
	}
}

func TestDecodeKeepsServerKeyOrder(t *testing.T) {
	v, err := Decode([]byte(`{"zeta":1,"alpha":{"b":true,"a":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	obj := v.(Object)
	if obj[0].Key != "zeta" || obj[1].Key != "alpha" || obj[1].Value.(Object)[0].Key != "b" {
		t.Fatalf("keys reordered: %v", obj)
	}
}

func TestPrettyExpandsJSONStoredInsideStrings(t *testing.T) {
	out := Pretty(Object{{"profile", `{"lang":"go"}`}})
	if !strings.Contains(out, `"lang"`) || strings.Contains(out, `\"lang\"`) {
		t.Fatalf("a JSON document stored in a text column must render as JSON, got %s", out)
	}
}

func TestPrettyKeepsShortScalarArraysOnOneLine(t *testing.T) {
	out := Pretty(Object{{"tags", []any{"tui", "sql"}}})
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("a short tag list spread over many lines hides the rest of the document, got %q", out)
	}
}

func TestPrettyEscapesTviewTags(t *testing.T) {
	out := Pretty("[red]")
	if !strings.Contains(out, "[red[]") {
		t.Fatalf("user data must not be interpreted as color tags, got %s", out)
	}
}

func TestScalarRendersNullAndBytes(t *testing.T) {
	if Scalar(nil) != "NULL" || Scalar([]byte("hi")) != "hi" {
		t.Fatal("NULL and text bytes should be readable in tables")
	}
}

func TestPromQLHighlightsFunctionsLabelsAndDurations(t *testing.T) {
	k := kinds(PromQL, `sum by (job) (rate(http_requests_total{job="api"}[5m]))`)
	if k["rate"] != Function || k["sum"] != Function || k["by"] != Keyword || k["job"] != Label || k["5m"] != Number {
		t.Fatalf("got %v", k)
	}
}
