package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/diegopacheco/dev-cli/internal/sys"
)

type Found struct {
	Kind      string
	Container string
	Image     string
	HostPort  int
	Target    string
}

type rule struct {
	kind   string
	tokens []string
	port   int
	target func(host string, port int, env map[string]string) string
}

var rules = []rule{
	{"Postgres", []string{"postgres", "postgis", "timescaledb", "pgvector"}, 5432, postgresTarget},
	{"MySQL", []string{"mysql", "mariadb", "percona"}, 3306, mysqlTarget},
	{"Cassandra", []string{"cassandra", "scylla"}, 9042, func(h string, p int, _ map[string]string) string { return fmt.Sprintf("%s:%d", h, p) }},
	{"Redis", []string{"redis", "valkey", "keydb"}, 6379, redisTarget},
	{"Loki", []string{"loki"}, 3100, httpTarget},
	{"Grafana", []string{"grafana"}, 3000, grafanaTarget},
	{"Prometheus", []string{"prometheus"}, 9090, httpTarget},
}

type inspected struct {
	Name      string `json:"Name"`
	ImageName string `json:"ImageName"`
	Config    struct {
		Image string   `json:"Image"`
		Env   []string `json:"Env"`
	} `json:"Config"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
}

func ImageName(image string) string {
	name := image
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name, _, _ = strings.Cut(name, "@")
	name, _, _ = strings.Cut(name, ":")
	return strings.ToLower(name)
}

func match(image string) (rule, bool) {
	name := ImageName(image)
	if strings.Contains(name, "exporter") {
		return rule{}, false
	}
	for _, r := range rules {
		for _, t := range r.tokens {
			if name == t || strings.HasPrefix(name, t+"-") || strings.HasPrefix(name, t+"_") {
				return r, true
			}
		}
	}
	return rule{}, false
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			m[k] = v
		}
	}
	return m
}

func first(m map[string]string, fallback string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return fallback
}

func userinfo(user, pass string) string {
	if pass == "" {
		return url.PathEscape(user)
	}
	return url.UserPassword(user, pass).String()
}

func postgresTarget(host string, port int, env map[string]string) string {
	user := first(env, "postgres", "POSTGRES_USER")
	db := first(env, user, "POSTGRES_DB")
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable", userinfo(user, env["POSTGRES_PASSWORD"]), host, port, db)
}

func mysqlTarget(host string, port int, env map[string]string) string {
	user, pass := "root", first(env, "", "MYSQL_ROOT_PASSWORD", "MARIADB_ROOT_PASSWORD")
	if pass == "" && env["MYSQL_USER"] != "" {
		user, pass = env["MYSQL_USER"], env["MYSQL_PASSWORD"]
	}
	return fmt.Sprintf("mysql://%s@%s:%d/%s", userinfo(user, pass), host, port, first(env, "", "MYSQL_DATABASE", "MARIADB_DATABASE"))
}

func redisTarget(host string, port int, env map[string]string) string {
	if pass := first(env, "", "REDIS_PASSWORD"); pass != "" {
		return fmt.Sprintf("redis://:%s@%s:%d/0", url.QueryEscape(pass), host, port)
	}
	return fmt.Sprintf("redis://%s:%d/0", host, port)
}

func httpTarget(host string, port int, _ map[string]string) string {
	return fmt.Sprintf("http://%s:%d", host, port)
}

func grafanaTarget(host string, port int, env map[string]string) string {
	user := first(env, "admin", "GF_SECURITY_ADMIN_USER")
	pass := first(env, "admin", "GF_SECURITY_ADMIN_PASSWORD")
	return fmt.Sprintf("http://%s@%s:%d", userinfo(user, pass), host, port)
}

func Parse(out string) ([]Found, error) {
	var list []inspected
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, err
	}
	var found []Found
	for _, c := range list {
		image := c.ImageName
		if image == "" {
			image = c.Config.Image
		}
		r, ok := match(image)
		if !ok || !c.State.Running {
			continue
		}
		bindings := c.NetworkSettings.Ports[strconv.Itoa(r.port)+"/tcp"]
		if len(bindings) == 0 || bindings[0].HostPort == "" {
			continue
		}
		hostPort, err := strconv.Atoi(bindings[0].HostPort)
		if err != nil {
			continue
		}
		host := bindings[0].HostIP
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		found = append(found, Found{
			Kind:      r.kind,
			Container: strings.TrimPrefix(c.Name, "/"),
			Image:     image,
			HostPort:  hostPort,
			Target:    r.target(host, hostPort, envMap(c.Config.Env)),
		})
	}
	order := map[string]int{}
	for i, r := range rules {
		order[r.kind] = i
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Kind != found[j].Kind {
			return order[found[i].Kind] < order[found[j].Kind]
		}
		return found[i].Container < found[j].Container
	})
	return found, nil
}

func Containers(ctx context.Context, run sys.Runner) ([]Found, error) {
	runtime, err := sys.Runtime()
	if err != nil {
		return nil, err
	}
	ids, err := run(ctx, runtime, "ps", "-q")
	if err != nil {
		return nil, err
	}
	args := append([]string{"inspect"}, strings.Fields(ids)...)
	if len(args) == 1 {
		return nil, nil
	}
	out, err := run(ctx, runtime, args...)
	if err != nil {
		return nil, err
	}
	return Parse(out)
}
