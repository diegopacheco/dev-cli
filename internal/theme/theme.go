package theme

import "github.com/gdamore/tcell/v2"

const (
	Bg      = "#0b0f1a"
	Panel   = "#11172a"
	Border  = "#2a3355"
	Cyan    = "#00e5ff"
	Magenta = "#ff4fd8"
	Lime    = "#a6ff4d"
	Orange  = "#ffb86c"
	Purple  = "#bd93f9"
	Red     = "#ff5c7a"
	Yellow  = "#ffe66d"
	Blue    = "#5c9dff"
	Dim     = "#6b7394"
	Text    = "#d7def0"
)

func Color(hex string) tcell.Color {
	return tcell.GetColor(hex)
}

func Lerp(a, b string, t float64) tcell.Color {
	ar, ag, ab := Color(a).RGB()
	br, bg, bb := Color(b).RGB()
	mix := func(x, y int32) int32 { return x + int32(float64(y-x)*t) }
	return tcell.NewRGBColor(mix(ar, br), mix(ag, bg), mix(ab, bb))
}

func Heat(t float64) tcell.Color {
	switch {
	case t < 0:
		t = 0
	case t > 1:
		t = 1
	}
	if t < 0.5 {
		return Lerp(Lime, Yellow, t*2)
	}
	return Lerp(Yellow, Red, (t-0.5)*2)
}

func Hex(c tcell.Color) string {
	r, g, b := c.RGB()
	return "#" + hex2(r) + hex2(g) + hex2(b)
}

func hex2(v int32) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[v>>4&0xf], digits[v&0xf]})
}
