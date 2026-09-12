package syntax

import (
	"sort"
	"strings"
	"unicode"
)

func WordBefore(line []rune, col int) (string, int) {
	start := col
	for start > 0 {
		r := line[start-1]
		if r == '_' || r == '.' || r == '$' || r == ':' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			start--
			continue
		}
		break
	}
	return string(line[start:col]), start
}

func Complete(prefix string, candidates []string, limit int) []string {
	if prefix == "" {
		return nil
	}
	lower := strings.ToLower(prefix)
	seen := map[string]bool{}
	var starts, contains []string
	for _, c := range candidates {
		lc := strings.ToLower(c)
		if seen[lc] || lc == lower {
			continue
		}
		switch {
		case strings.HasPrefix(lc, lower):
			seen[lc] = true
			starts = append(starts, c)
		case len(lower) > 1 && strings.Contains(lc, lower):
			seen[lc] = true
			contains = append(contains, c)
		}
	}
	rank := func(s []string) {
		sort.Slice(s, func(i, j int) bool {
			if len(s[i]) != len(s[j]) {
				return len(s[i]) < len(s[j])
			}
			return s[i] < s[j]
		})
	}
	rank(starts)
	rank(contains)
	out := append(starts, contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func MatchCase(prefix, word string, lang Language) string {
	if !lang.CaseFold || lang.lookup(word) == Plain {
		return word
	}
	for _, r := range prefix {
		if unicode.IsLetter(r) {
			if unicode.IsLower(r) {
				return strings.ToLower(word)
			}
			return strings.ToUpper(word)
		}
	}
	return word
}
