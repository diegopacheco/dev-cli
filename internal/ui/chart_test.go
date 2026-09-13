package ui

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/diegopacheco/dev-cli/internal/backend"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

var tagPattern = regexp.MustCompile(`\[[^\[\]]*\]`)

func plain(tagged string) []string {
	return strings.Split(strings.TrimRight(tagPattern.ReplaceAllString(tagged, ""), "\n"), "\n")
}

func rising(n int) backend.Series {
	s := backend.Series{Name: "api", Step: 1000}
	for i := range n {
		s.Times = append(s.Times, int64(i)*1000)
		s.Values = append(s.Values, float64(i))
	}
	return s
}

func renderLines(c *backend.Chart, width int) []string {
	var b strings.Builder
	renderChart(&b, c, width)
	return plain(b.String())
}

func TestTimeseriesPutsTheLowestPointBottomLeftAndTheHighestTopRight(t *testing.T) {
	lines := renderLines(&backend.Chart{Kind: "timeseries", Series: []backend.Series{rising(11)}}, 60)
	top, bottom := []rune(lines[0]), []rune(lines[chartHeight-1])
	if !strings.HasPrefix(lines[0], "10 ┤") || !strings.HasPrefix(lines[chartHeight-1], " 0 ┤") || !strings.HasPrefix(lines[chartHeight/2], " 5 ┤") {
		t.Fatalf("the axis must label max, midpoint and min on their rows:\n%s", strings.Join(lines, "\n"))
	}
	if top[len(top)-1] == ' ' || bottom[4] == ' ' || top[4] != ' ' {
		t.Fatalf("a rising series must start bottom left and end top right:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[len(lines)-1], "last 10") || !strings.Contains(lines[len(lines)-1], "min 0") {
		t.Fatalf("the legend must show last, min and max: %q", lines[len(lines)-1])
	}
	for _, l := range lines[:chartHeight] {
		if w := runewidth.StringWidth(l); w != 60 {
			t.Fatalf("every plot row must fill the pane width exactly, got %d: %q", w, l)
		}
	}
}

func TestTimeseriesLeavesAGapWhereSamplesAreMissing(t *testing.T) {
	s := backend.Series{Name: "api", Step: 1000, Times: []int64{0, 1000, 20000, 21000}, Values: []float64{5, 5, 5, 5}}
	lines := renderLines(&backend.Chart{Kind: "timeseries", Series: []backend.Series{s}}, 60)
	row := []rune(lines[0])
	gap := strings.TrimSpace(string(row[10:40]))
	if gap != "" || row[4] == ' ' || row[len(row)-1] == ' ' {
		t.Fatalf("a missing scrape must not be drawn as a flat line:\n%s", strings.Join(lines, "\n"))
	}
	nulls := backend.Series{Name: "api", Times: []int64{0, 1000, 2000}, Values: []float64{1, math.NaN(), 1}}
	if got := renderLines(&backend.Chart{Kind: "timeseries", Series: []backend.Series{nulls}}, 60); strings.Count(got[0], "⠁")+strings.Count(got[0], "⠈") == 0 {
		t.Fatalf("null values break the line into points:\n%s", strings.Join(got, "\n"))
	}
}

func TestStatShowsTheLastValueInBigDigitsWithItsUnit(t *testing.T) {
	s := backend.Series{Name: "p95", Times: []int64{0, 1000}, Values: []float64{0.9, 0.25}}
	lines := renderLines(&backend.Chart{Kind: "stat", Unit: "s", Series: []backend.Series{s}}, 60)
	want := bigText("250")
	for i := range 3 {
		if !strings.Contains(lines[i], want[i]) {
			t.Fatalf("0.25s must read as 250ms in big digits, row %d = %q", i, lines[i])
		}
	}
	if !strings.HasSuffix(lines[2], "ms") || !strings.Contains(lines[3], "p95") {
		t.Fatalf("unit and name must follow the number: %q", lines)
	}
}

func TestGaugeFillsInProportionToItsMax(t *testing.T) {
	c := &backend.Chart{Kind: "gauge", Unit: "percent", Series: []backend.Series{
		{Name: "half", Times: []int64{0}, Values: []float64{50}},
		{Name: "most", Times: []int64{0}, Values: []float64{80}},
	}}
	lines := renderLines(c, 60)
	for i, ratio := range []float64{0.5, 0.8} {
		filled := strings.Count(lines[i], "■")
		bar := filled + strings.Count(lines[i], "·")
		if filled != int(math.Round(ratio*float64(bar))) {
			t.Fatalf("a percent gauge fills against 100, not against the largest value, got %d of %d:\n%s", filled, bar, strings.Join(lines, "\n"))
		}
	}
}

func TestFormatValueUsesGrafanaUnits(t *testing.T) {
	cases := map[string]string{
		formatValue(1536, "bytes"):       "1.5K",
		formatValue(0.25, "percentunit"): "25%",
		formatValue(12.345, "percent"):   "12.3%",
		formatValue(0.004, "s"):          "4ms",
		formatValue(1250000, ""):         "1.25M",
		formatValue(3, ""):               "3",
		formatValue(math.NaN(), ""):      "n/a",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestChartFollowsTheResultsPaneWidthAfterAResize(t *testing.T) {
	a := testApp()
	c, _ := a.console("Grafana")
	c.runDone([]backend.Result{{Title: "latency", Chart: &backend.Chart{Kind: "timeseries", Series: []backend.Series{rising(30)}}}}, nil, 0)
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	width := func(w int) int {
		screen.SetSize(w+2, 40)
		c.output.SetRect(0, 0, w+2, 40)
		c.output.Draw(screen)
		axis := ""
		for _, l := range plain(c.output.GetText(false)) {
			if strings.Contains(l, "└") {
				axis = l
			}
		}
		return runewidth.StringWidth(axis)
	}
	narrow, wide := width(60), width(120)
	if narrow != 60 || wide != 120 {
		t.Fatalf("a chart drawn for one pane width must be redrawn when the pane changes, got %d then %d", narrow, wide)
	}
}
