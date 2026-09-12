package syntax

import (
	"strings"
	"unicode"
)

type Kind int

const (
	Plain Kind = iota
	Keyword
	Function
	String
	Number
	Comment
	Operator
	Variable
	Label
	Punct
)

type Token struct {
	Kind  Kind
	Start int
	End   int
}

type Language struct {
	Name         string
	Keywords     map[string]bool
	Functions    map[string]bool
	LineComments []string
	BlockComment bool
	CaseFold     bool
	Operators    []string
	WordRunes    string
	Variables    bool
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[strings.ToUpper(w)] = true
	}
	return m
}

func (l Language) Words() []string {
	out := make([]string, 0, len(l.Keywords)+len(l.Functions))
	for w := range l.Keywords {
		out = append(out, w)
	}
	for w := range l.Functions {
		out = append(out, w)
	}
	return out
}

func (l Language) lookup(word string) Kind {
	key := strings.ToUpper(word)
	if l.Keywords[key] {
		return Keyword
	}
	if l.Functions[key] {
		return Function
	}
	return Plain
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func Lex(lang Language, line []rune, inBlock bool) ([]Token, bool) {
	var tokens []Token
	i := 0
	n := len(line)
	if inBlock {
		end := indexOf(line, 0, "*/")
		if end < 0 {
			return []Token{{Comment, 0, n}}, true
		}
		tokens = append(tokens, Token{Comment, 0, end + 2})
		i = end + 2
	}
	for i < n {
		r := line[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case lang.BlockComment && hasPrefix(line, i, "/*"):
			end := indexOf(line, i+2, "*/")
			if end < 0 {
				tokens = append(tokens, Token{Comment, i, n})
				return tokens, true
			}
			tokens = append(tokens, Token{Comment, i, end + 2})
			i = end + 2
		case lineComment(lang, line, i):
			tokens = append(tokens, Token{Comment, i, n})
			i = n
		case r == '\'' || r == '"' || r == '`':
			end := closeQuote(line, i)
			tokens = append(tokens, Token{String, i, end})
			i = end
		case unicode.IsDigit(r):
			end := i + 1
			for end < n && (unicode.IsDigit(line[end]) || line[end] == '.' || unicode.IsLetter(line[end])) {
				end++
			}
			tokens = append(tokens, Token{Number, i, end})
			i = end
		case lang.Variables && (r == '$' || r == ':' || r == '@') && i+1 < n && isWordRune(line[i+1]):
			end := i + 1
			for end < n && isWordRune(line[end]) {
				end++
			}
			tokens = append(tokens, Token{Variable, i, end})
			i = end
		case isWordRune(r) || strings.ContainsRune(lang.WordRunes, r) && i+1 < n && unicode.IsLetter(line[i+1]):
			end := i
			for end < n && (isWordRune(line[end]) || line[end] == '.' || strings.ContainsRune(lang.WordRunes, line[end])) {
				end++
			}
			kind := lang.lookup(string(line[i:end]))
			if kind == Plain && (lang.Name == "logql" || lang.Name == "promql") && end < n && (line[end] == '=' || line[end] == '!') {
				kind = Label
			}
			tokens = append(tokens, Token{kind, i, end})
			i = end
		default:
			if op := operator(lang, line, i); op > 0 {
				tokens = append(tokens, Token{Operator, i, i + op})
				i += op
				continue
			}
			tokens = append(tokens, Token{Punct, i, i + 1})
			i++
		}
	}
	return tokens, false
}

func lineComment(lang Language, line []rune, i int) bool {
	for _, c := range lang.LineComments {
		if hasPrefix(line, i, c) {
			return true
		}
	}
	return false
}

func operator(lang Language, line []rune, i int) int {
	best := 0
	for _, op := range lang.Operators {
		if len([]rune(op)) > best && hasPrefix(line, i, op) {
			best = len([]rune(op))
		}
	}
	return best
}

func closeQuote(line []rune, i int) int {
	q := line[i]
	j := i + 1
	for j < len(line) {
		if line[j] == '\\' {
			j += 2
			continue
		}
		if line[j] == q {
			return j + 1
		}
		j++
	}
	return len(line)
}

func hasPrefix(line []rune, i int, p string) bool {
	pr := []rune(p)
	if i+len(pr) > len(line) {
		return false
	}
	for k, r := range pr {
		if line[i+k] != r {
			return false
		}
	}
	return true
}

func indexOf(line []rune, from int, p string) int {
	for i := from; i < len(line); i++ {
		if hasPrefix(line, i, p) {
			return i
		}
	}
	return -1
}
