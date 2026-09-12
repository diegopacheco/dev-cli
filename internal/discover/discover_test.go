package discover

import (
	"strings"
	"testing"
)

const inspectOut = `[
 {"Name":"devcli-postgres","ImageName":"docker.io/library/postgres:18","Config":{"Env":["POSTGRES_PASSWORD=devcli","POSTGRES_DB=devcli"]},"NetworkSettings":{"Ports":{"5432/tcp":[{"HostIp":"0.0.0.0","HostPort":"5432"}]}},"State":{"Running":true}},
 {"Name":"/web-grafana","Config":{"Image":"grafana/grafana:11.0.0","Env":[]},"NetworkSettings":{"Ports":{"3000/tcp":[{"HostIp":"","HostPort":"3001"}]}},"State":{"Running":true}},
 {"Name":"logs","ImageName":"docker.io/grafana/loki:3.5.5","Config":{"Env":[]},"NetworkSettings":{"Ports":{"3100/tcp":[{"HostIp":"0.0.0.0","HostPort":"3100"}]}},"State":{"Running":true}},
 {"Name":"pg-exporter","ImageName":"quay.io/prometheuscommunity/postgres-exporter:latest","Config":{"Env":[]},"NetworkSettings":{"Ports":{"9187/tcp":[{"HostIp":"0.0.0.0","HostPort":"9187"}]}},"State":{"Running":true}},
 {"Name":"cache","ImageName":"docker.io/library/redis:8","Config":{"Env":[]},"NetworkSettings":{"Ports":{"6379/tcp":[{"HostIp":"0.0.0.0","HostPort":"6380"}]}},"State":{"Running":true}},
 {"Name":"internal-redis","ImageName":"docker.io/library/redis:8","Config":{"Env":[]},"NetworkSettings":{"Ports":{}},"State":{"Running":true}},
 {"Name":"shop-mysql","ImageName":"docker.io/library/mysql:9","Config":{"Env":["MYSQL_ROOT_PASSWORD=p@ss","MYSQL_DATABASE=shop"]},"NetworkSettings":{"Ports":{"3306/tcp":[{"HostIp":"127.0.0.1","HostPort":"13306"}]}},"State":{"Running":true}},
 {"Name":"ring","ImageName":"docker.io/library/cassandra:5.0","Config":{"Env":[]},"NetworkSettings":{"Ports":{"9042/tcp":[{"HostIp":"0.0.0.0","HostPort":"9042"}]}},"State":{"Running":true}},
 {"Name":"prom","ImageName":"quay.io/prometheus/prometheus:latest","Config":{"Env":[]},"NetworkSettings":{"Ports":{"9090/tcp":[{"HostIp":"0.0.0.0","HostPort":"9090"}]}},"State":{"Running":true}}
]`

func TestParseBuildsConnectTargetsFromContainerEnvAndPorts(t *testing.T) {
	found, err := Parse(inspectOut)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Found{}
	for _, f := range found {
		got[f.Container] = f
	}
	cases := map[string]string{
		"devcli-postgres": "postgres://postgres:devcli@127.0.0.1:5432/devcli?sslmode=disable",
		"web-grafana":     "http://admin:admin@127.0.0.1:3001",
		"logs":            "http://127.0.0.1:3100",
		"cache":           "redis://127.0.0.1:6380/0",
		"shop-mysql":      "mysql://root:p%40ss@127.0.0.1:13306/shop",
		"ring":            "127.0.0.1:9042",
		"prom":            "http://127.0.0.1:9090",
	}
	for name, target := range cases {
		if got[name].Target != target {
			t.Errorf("%s: got %q want %q", name, got[name].Target, target)
		}
	}
	if _, ok := got["pg-exporter"]; ok {
		t.Error("an exporter is not a database even when its name says postgres")
	}
	if _, ok := got["internal-redis"]; ok {
		t.Error("a container without a published port cannot be reached from the host")
	}
	if got["logs"].Kind != "Loki" {
		t.Errorf("grafana/loki must be Loki, not Grafana, got %s", got["logs"].Kind)
	}
	if found[0].Kind != "Postgres" || found[len(found)-1].Kind != "Prometheus" {
		t.Errorf("results must follow the tab order, got %s..%s", found[0].Kind, found[len(found)-1].Kind)
	}
}

func TestImageNameStripsRegistryTagAndDigest(t *testing.T) {
	if ImageName("quay.io/prometheus/prometheus:v3@sha256:abc") != "prometheus" {
		t.Fatal(ImageName("quay.io/prometheus/prometheus:v3@sha256:abc"))
	}
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	if _, err := Parse("not json"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("got %v", err)
	}
}
