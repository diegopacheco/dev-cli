package ui

import (
	"strings"
	"unicode"
)

func wordStart(runes []rune, i int) bool {
	if i == 0 {
		return true
	}
	p := runes[i-1]
	return !unicode.IsLetter(p) && !unicode.IsDigit(p) || unicode.IsLower(p) && unicode.IsUpper(runes[i])
}

func matchToken(token string, text []rune, lower []rune) (int, []int) {
	tok := []rune(token)
	if idx := strings.Index(string(lower), token); idx >= 0 {
		start := len([]rune(string(lower)[:idx]))
		positions := make([]int, len(tok))
		for i := range tok {
			positions[i] = start + i
		}
		score := 40 + len(tok)*6
		if start == 0 {
			score += 40
		} else if wordStart(text, start) {
			score += 25
		}
		return score, positions
	}
	var positions []int
	score := 0
	j := 0
	for i := 0; i < len(lower) && j < len(tok); i++ {
		if lower[i] != tok[j] {
			continue
		}
		score += 2
		if wordStart(text, i) {
			score += 8
		}
		if len(positions) > 0 && positions[len(positions)-1] == i-1 {
			score += 5
		}
		positions = append(positions, i)
		j++
	}
	if j < len(tok) {
		return -1, nil
	}
	return score - (positions[len(positions)-1]-positions[0])/4, positions
}

func Fuzzy(query, title, detail string) (int, []int) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0, nil
	}
	titleRunes := []rune(title)
	titleLower := []rune(strings.ToLower(title))
	detailRunes := []rune(detail)
	detailLower := []rune(strings.ToLower(detail))
	total := 0
	var highlight []int
	for _, token := range strings.Fields(query) {
		score, positions := matchToken(token, titleRunes, titleLower)
		if score >= 0 {
			total += score * 2
			highlight = append(highlight, positions...)
			continue
		}
		score, _ = matchToken(token, detailRunes, detailLower)
		if score < 0 {
			return -1, nil
		}
		total += score
	}
	return total - len(titleRunes)/8, highlight
}
