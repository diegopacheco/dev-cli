package syntax

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/diegopacheco/dev-cli/internal/theme"
	"github.com/rivo/tview"
)

type Pair struct {
	Key   string
	Value any
}

type Object []Pair

func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := Object{}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj = append(obj, Pair{k.(string), v})
			}
			_, err := dec.Token()
			return obj, err
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			_, err := dec.Token()
			return arr, err
		}
	}
	return tok, nil
}

func ParseEmbedded(s string) (any, bool) {
	t := strings.TrimSpace(s)
	if len(t) < 2 || !(t[0] == '{' && t[len(t)-1] == '}' || t[0] == '[' && t[len(t)-1] == ']') {
		return nil, false
	}
	v, err := Decode([]byte(t))
	return v, err == nil
}

func Normalize(v any, expand bool) any {
	switch x := v.(type) {
	case nil, bool, json.Number:
		return x
	case string:
		if expand {
			if inner, ok := ParseEmbedded(x); ok {
				return Normalize(inner, expand)
			}
		}
		return x
	case []byte:
		if utf8.Valid(x) {
			return Normalize(string(x), expand)
		}
		return fmt.Sprintf("0x%x", x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return json.Number(fmt.Sprint(x))
	case float32:
		return json.Number(strconv.FormatFloat(float64(x), 'g', -1, 32))
	case float64:
		return json.Number(strconv.FormatFloat(x, 'g', -1, 64))
	case Object:
		out := make(Object, len(x))
		for i, p := range x {
			out[i] = Pair{p.Key, Normalize(p.Value, expand)}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = Normalize(e, expand)
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(Object, 0, len(x))
		for _, k := range keys {
			out = append(out, Pair{k, Normalize(x[k], expand)})
		}
		return out
	case fmt.Stringer:
		return x.String()
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	decoded, err := Decode(data)
	if err != nil {
		return fmt.Sprint(v)
	}
	return Normalize(decoded, expand)
}

func Scalar(v any) string {
	switch x := Normalize(v, false).(type) {
	case nil:
		return "NULL"
	case string:
		return x
	case json.Number:
		return string(x)
	case bool:
		return strconv.FormatBool(x)
	default:
		var b strings.Builder
		writeCompact(&b, x)
		return b.String()
	}
}

func writeCompact(b *strings.Builder, v any) {
	switch x := v.(type) {
	case Object:
		b.WriteByte('{')
		for i, p := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(quote(p.Key))
			b.WriteByte(':')
			writeCompact(b, p.Value)
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCompact(b, e)
		}
		b.WriteByte(']')
	case string:
		b.WriteString(quote(x))
	case nil:
		b.WriteString("null")
	default:
		b.WriteString(fmt.Sprint(x))
	}
}

func Pretty(v any) string {
	var b strings.Builder
	writePretty(&b, Normalize(v, true), 0)
	return b.String()
}

func tag(color, text string) string {
	return "[" + color + "]" + tview.Escape(text) + "[-]"
}

func writePretty(b *strings.Builder, v any, depth int) {
	indent := strings.Repeat("  ", depth+1)
	closing := strings.Repeat("  ", depth)
	switch x := v.(type) {
	case Object:
		if len(x) == 0 {
			b.WriteString(tag(theme.Dim, "{}"))
			return
		}
		b.WriteString(tag(theme.Dim, "{") + "\n")
		for i, p := range x {
			b.WriteString(indent + tag(theme.Cyan, quote(p.Key)) + tag(theme.Dim, ": "))
			writePretty(b, p.Value, depth+1)
			if i < len(x)-1 {
				b.WriteString(tag(theme.Dim, ","))
			}
			b.WriteByte('\n')
		}
		b.WriteString(closing + tag(theme.Dim, "}"))
	case []any:
		if len(x) == 0 {
			b.WriteString(tag(theme.Dim, "[]"))
			return
		}
		if inlineArray(x) {
			writeColoredCompact(b, x)
			return
		}
		b.WriteString(tag(theme.Dim, "[") + "\n")
		for i, e := range x {
			b.WriteString(indent)
			writePretty(b, e, depth+1)
			if i < len(x)-1 {
				b.WriteString(tag(theme.Dim, ","))
			}
			b.WriteByte('\n')
		}
		b.WriteString(closing + tag(theme.Dim, "]"))
	case string:
		b.WriteString(tag(theme.Lime, quote(x)))
	case json.Number:
		b.WriteString(tag(theme.Orange, string(x)))
	case bool:
		b.WriteString(tag(theme.Magenta, strconv.FormatBool(x)))
	case nil:
		b.WriteString(tag(theme.Dim, "null"))
	default:
		b.WriteString(tview.Escape(fmt.Sprint(x)))
	}
}

func inlineArray(arr []any) bool {
	width := 0
	for _, e := range arr {
		switch x := e.(type) {
		case Object, []any:
			return false
		case string:
			width += len(x) + 4
		default:
			width += 8
		}
	}
	return width <= 60
}

func Compact(v any) string {
	var b strings.Builder
	writeColoredCompact(&b, Normalize(v, true))
	return b.String()
}

func writeColoredCompact(b *strings.Builder, v any) {
	switch x := v.(type) {
	case Object:
		b.WriteString(tag(theme.Dim, "{"))
		for i, p := range x {
			if i > 0 {
				b.WriteString(tag(theme.Dim, ", "))
			}
			b.WriteString(tag(theme.Cyan, quote(p.Key)) + tag(theme.Dim, ":"))
			writeColoredCompact(b, p.Value)
		}
		b.WriteString(tag(theme.Dim, "}"))
	case []any:
		b.WriteString(tag(theme.Dim, "["))
		for i, e := range x {
			if i > 0 {
				b.WriteString(tag(theme.Dim, ", "))
			}
			writeColoredCompact(b, e)
		}
		b.WriteString(tag(theme.Dim, "]"))
	default:
		writePretty(b, x, 0)
	}
}

func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
