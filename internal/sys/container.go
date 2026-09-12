package sys

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Container struct {
	ID     string
	Name   string
	Image  string
	State  string
	Status string
	Ports  string
}

func Runtime() (string, error) {
	for _, name := range []string{"podman", "docker"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("neither podman nor docker found on PATH")
}

type podmanPort struct {
	HostIP        string `json:"host_ip"`
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"`
	Protocol      string `json:"protocol"`
}

type podmanContainer struct {
	ID     string       `json:"Id"`
	Names  []string     `json:"Names"`
	Image  string       `json:"Image"`
	State  string       `json:"State"`
	Status string       `json:"Status"`
	Ports  []podmanPort `json:"Ports"`
}

type dockerContainer struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
	Ports  string `json:"Ports"`
}

func ParsePodman(out string) ([]Container, error) {
	var raw []podmanContainer
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	list := make([]Container, 0, len(raw))
	for _, c := range raw {
		var ports []string
		for _, p := range c.Ports {
			ports = append(ports, fmt.Sprintf("%d->%d/%s", p.HostPort, p.ContainerPort, p.Protocol))
		}
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		list = append(list, Container{ID: id, Name: strings.Join(c.Names, ","), Image: c.Image, State: c.State, Status: c.Status, Ports: strings.Join(ports, " ")})
	}
	return list, nil
}

func ParseDocker(out string) ([]Container, error) {
	var list []Container
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var c dockerContainer
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, err
		}
		list = append(list, Container{ID: c.ID, Name: c.Names, Image: c.Image, State: c.State, Status: c.Status, Ports: c.Ports})
	}
	return list, nil
}

func Containers(ctx context.Context, run Runner, runtime string) ([]Container, error) {
	if strings.HasSuffix(runtime, "docker") {
		out, err := run(ctx, runtime, "ps", "-a", "--format", "{{json .}}")
		if err != nil {
			return nil, err
		}
		return ParseDocker(out)
	}
	out, err := run(ctx, runtime, "ps", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	return ParsePodman(out)
}

type Action string

const (
	Stop   Action = "stop"
	Start  Action = "start"
	Kill   Action = "kill"
	Remove Action = "remove"
)

func ActionArgs(action Action, id string) []string {
	switch action {
	case Stop:
		return []string{"stop", "-t", "5", id}
	case Start:
		return []string{"start", id}
	case Kill:
		return []string{"kill", id}
	case Remove:
		return []string{"rm", "-f", id}
	}
	return nil
}

func ContainerAction(ctx context.Context, run Runner, runtime string, action Action, id string) error {
	args := ActionArgs(action, id)
	if args == nil {
		return fmt.Errorf("unknown action %s", action)
	}
	_, err := run(ctx, runtime, args...)
	return err
}

func ShellCommand(runtime, id string) *exec.Cmd {
	return exec.Command(runtime, "exec", "-it", id, "sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh")
}
