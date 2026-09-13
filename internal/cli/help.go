package cli

import (
	"fmt"
	"strings"

	"github.com/diegopacheco/dev-cli/internal/theme"
)

type helpRow struct {
	usage string
	desc  string
}

type helpSection struct {
	title string
	color string
	rows  []helpRow
}

var helpSections = []helpSection{
	{"USAGE", theme.Cyan, []helpRow{
		{"devcli", "open the TUI · Cmd-K search anything · Cmd-/ shortcuts"},
		{"devcli [options] MODE [QUERY]", "run once, print the result and exit; QUERY can also come from stdin"},
	}},
	{"MODES", theme.Magenta, []helpRow{
		{"-sql --postgres QUERY", "run SQL on Postgres"},
		{"-sql --mysql QUERY", "run SQL on MySQL"},
		{"--sqlite QUERY", "run SQL on SQLite (also --sqllite)"},
		{"-cassandra QUERY", "run CQL (also -cql)"},
		{"-redis COMMAND", "run Redis commands, one per line"},
		{"-loki LOGQL", "query Loki · all · labels · values LABEL · :range 30m · :limit 100"},
		{"-grafana COMMAND", "all · health · search · dashboard UID · datasources · query DS EXPR · get /api/..."},
		{"-prometheus PROMQL", "instant query · all · metrics · labels · targets · alerts · :range 1h"},
		{"-ps [FILTER]", "list processes sorted by CPU"},
		{"-containers", "list podman / docker containers"},
		{"-threads [PID]", "list JVMs, or dump the threads of PID"},
		{"-discover", "find database and observability containers and their connect targets"},
	}},
	{"OPTIONS", theme.Lime, []helpRow{
		{"-json", "print results as JSON"},
		{"-target URL", "connect to URL instead of the environment default"},
		{"-timeout 30s", "give up after this long"},
		{"-no-color", "plain output (NO_COLOR is honored too)"},
		{"-q, -quiet", "no banner on stderr"},
		{"-tab NAME", "TUI starts on this tab"},
		{"-no-discover", "TUI does not scan containers at start"},
		{"-capture DIR", "render every TUI tab to HTML and exit"},
		{"-version, -help", "version, this help"},
	}},
	{"ENVIRONMENT", theme.Orange, []helpRow{
		{"DEVCLI_POSTGRES", "postgres://postgres:devcli@127.0.0.1:5432/devcli?sslmode=disable"},
		{"DEVCLI_MYSQL", "mysql://root:devcli@127.0.0.1:3306/devcli"},
		{"DEVCLI_SQLITE", "devcli.db"},
		{"DEVCLI_CASSANDRA", "127.0.0.1:9042/devcli"},
		{"DEVCLI_REDIS", "redis://127.0.0.1:6380/0"},
		{"DEVCLI_LOKI", "http://127.0.0.1:3100"},
		{"DEVCLI_GRAFANA", "http://admin:devcli@127.0.0.1:3000 (DEVCLI_GRAFANA_TOKEN for a bearer token)"},
		{"DEVCLI_PROMETHEUS", "http://127.0.0.1:9090"},
	}},
	{"QUICK RUNS", theme.Purple, []helpRow{
		{`devcli -sql --postgres "select * from users"`, ""},
		{`devcli -json --sqlite "select * from hosts" | jq .`, ""},
		{`devcli -redis HGETALL session:9f2c`, ""},
		{`devcli -loki all`, ""},
		{`devcli -loki '{app="api"} |= "error"'`, ""},
		{`devcli -prometheus 'sum by (job) (up)'`, ""},
		{`echo "GET user:1" | devcli -q -redis`, ""},
		{`devcli -threads $(pgrep -f DevcliJvm)`, ""},
	}},
}

func Help() string {
	var b strings.Builder
	for _, s := range helpSections {
		b.WriteString("  [" + s.color + "::b]" + s.title + "[-::-]\n")
		width := 0
		for _, r := range s.rows {
			if r.desc != "" {
				width = max(width, len([]rune(r.usage)))
			}
		}
		for _, r := range s.rows {
			usage := escape(r.usage)
			if r.desc == "" {
				b.WriteString("    [" + theme.Text + "]" + usage + "[-]\n")
				continue
			}
			pad := strings.Repeat(" ", width-len([]rune(r.usage)))
			b.WriteString(fmt.Sprintf("    [%s]%s[-]%s  [%s]%s[-]\n", s.color, usage, pad, theme.Dim, escape(r.desc)))
		}
		b.WriteString("\n")
	}
	b.WriteString("  [" + theme.Dim + "]every option works with one or two dashes · options go before the query[-]\n")
	return b.String()
}
