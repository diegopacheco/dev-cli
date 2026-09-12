package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/diegopacheco/dev-cli/internal/ui"
)

const version = "0.1.0"

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	tab := flag.String("tab", "dashboard", "tab to open: dashboard, processes, containers, threads, mysql, postgres, sqlite, cassandra, redis, loki, grafana")
	capture := flag.String("capture", "", "render every tab into HTML files in this directory and exit")
	showVersion := flag.Bool("version", false, "print the version")
	flag.Parse()
	if *showVersion {
		fmt.Println("devcli", version)
		return
	}
	targets := ui.Targets{
		MySQL:        env("DEVCLI_MYSQL", "mysql://root:devcli@127.0.0.1:3306/devcli"),
		Postgres:     env("DEVCLI_POSTGRES", "postgres://postgres:devcli@127.0.0.1:5432/devcli?sslmode=disable"),
		SQLite:       env("DEVCLI_SQLITE", "devcli.db"),
		Cassandra:    env("DEVCLI_CASSANDRA", "127.0.0.1:9042/devcli"),
		Redis:        env("DEVCLI_REDIS", "redis://127.0.0.1:6380/0"),
		Loki:         env("DEVCLI_LOKI", "http://127.0.0.1:3100"),
		Grafana:      env("DEVCLI_GRAFANA", "http://admin:devcli@127.0.0.1:3000"),
		GrafanaToken: os.Getenv("DEVCLI_GRAFANA_TOKEN"),
	}
	var err error
	if *capture != "" {
		err = ui.Capture(targets, *capture)
	} else {
		err = ui.New(targets, nil).Run(*tab)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "devcli:", err)
		os.Exit(1)
	}
}
