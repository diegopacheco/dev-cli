package backend

import (
	"context"
	"strings"
	"time"

	"github.com/diegopacheco/dev-cli/internal/syntax"
)

type Backend interface {
	Name() string
	Language() syntax.Language
	DefaultTarget() string
	Connect(ctx context.Context, target string) error
	Execute(ctx context.Context, input string) ([]Result, error)
	Words(ctx context.Context) []string
	RunsOnEnter(input string) bool
	Close() error
}

type LogLine struct {
	Time   string
	Labels syntax.Object
	Line   string
}

type Result struct {
	Title   string
	Columns []string
	Rows    [][]any
	Logs    []LogLine
	Text    string
	Value   any
	Message string
	Elapsed time.Duration
}

const MaxRows = 1000

func SplitStatements(input string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			cur.WriteRune(r)
			if r == '\\' && i+1 < len(runes) {
				i++
				cur.WriteRune(runes[i])
			} else if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
			cur.WriteRune(r)
		case r == '-' && i+1 < len(runes) && runes[i+1] == '-':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			cur.WriteRune('\n')
		case r == ';':
			if s := strings.TrimSpace(cur.String()); s != "" {
				out = append(out, s)
			}
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func EndsWithSemicolon(input string) bool {
	return strings.HasSuffix(strings.TrimSpace(input), ";")
}

func FirstWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return strings.ToUpper(f[0])
}

func Lines(input string) []string {
	var out []string
	for _, l := range strings.Split(input, "\n") {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			out = append(out, t)
		}
	}
	return out
}

func Unique(words []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(words))
	for _, w := range words {
		if w != "" && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}
