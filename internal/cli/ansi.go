package cli

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	tagPattern    = regexp.MustCompile(`^\[(#[0-9a-fA-F]{6}|-)?(?::(#[0-9a-fA-F]{6}|-)?)?(?::([bdiu]+|-))?\]`)
	escapePattern = regexp.MustCompile(`^\[[a-zA-Z0-9_,;: \-\."#]+\[*\[\]`)
)

type style struct {
	fg, bg, attrs string
}

func hexRGB(hex string) (int64, int64, int64) {
	n, _ := strconv.ParseInt(strings.TrimPrefix(hex, "#"), 16, 32)
	return n >> 16 & 0xff, n >> 8 & 0xff, n & 0xff
}

func (s style) sgr() string {
	codes := []string{"0"}
	for _, a := range s.attrs {
		switch a {
		case 'b':
			codes = append(codes, "1")
		case 'd':
			codes = append(codes, "2")
		case 'i':
			codes = append(codes, "3")
		case 'u':
			codes = append(codes, "4")
		}
	}
	if s.fg != "" {
		r, g, b := hexRGB(s.fg)
		codes = append(codes, fmt.Sprintf("38;2;%d;%d;%d", r, g, b))
	}
	if s.bg != "" {
		r, g, b := hexRGB(s.bg)
		codes = append(codes, fmt.Sprintf("48;2;%d;%d;%d", r, g, b))
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func apply(current, value string) string {
	switch value {
	case "":
		return current
	case "-":
		return ""
	}
	return value
}

func Colorize(tagged string, color bool) string {
	var b strings.Builder
	var st style
	styled := false
	for i := 0; i < len(tagged); {
		if tagged[i] != '[' {
			next := strings.IndexByte(tagged[i:], '[')
			if next < 0 {
				next = len(tagged) - i
			}
			b.WriteString(tagged[i : i+next])
			i += next
			continue
		}
		rest := tagged[i:]
		if m := tagPattern.FindStringSubmatch(rest); m != nil && len(m[0]) > 2 {
			st = style{fg: apply(st.fg, m[1]), bg: apply(st.bg, m[2]), attrs: apply(st.attrs, m[3])}
			if color {
				b.WriteString(st.sgr())
				styled = true
			}
			i += len(m[0])
			continue
		}
		if m := escapePattern.FindString(rest); m != "" {
			b.WriteString(m[:len(m)-2] + "]")
			i += len(m)
			continue
		}
		b.WriteByte('[')
		i++
	}
	if styled {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
