package sys

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type Process struct {
	PID     int
	PPID    int
	User    string
	CPU     float64
	Mem     float64
	RSS     int64
	Elapsed string
	Command string
}

func (p Process) Name() string {
	return filepath.Base(p.Command)
}

func ParsePS(out string) []Process {
	var procs []Process
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		cpu, _ := strconv.ParseFloat(f[3], 64)
		mem, _ := strconv.ParseFloat(f[4], 64)
		rss, _ := strconv.ParseInt(f[5], 10, 64)
		procs = append(procs, Process{PID: pid, PPID: ppid, User: f[2], CPU: cpu, Mem: mem, RSS: rss << 10, Elapsed: f[6], Command: strings.Join(f[7:], " ")})
	}
	return procs
}

func Processes(ctx context.Context, run Runner) ([]Process, error) {
	out, err := run(ctx, "ps", "-axo", "pid=,ppid=,user=,%cpu=,%mem=,rss=,etime=,comm=")
	if err != nil {
		return nil, err
	}
	return ParsePS(out), nil
}

type SortKey int

const (
	ByCPU SortKey = iota
	ByMem
	ByPID
)

func (k SortKey) String() string {
	return [...]string{"CPU", "MEM", "PID"}[k]
}

func SortProcesses(procs []Process, key SortKey) {
	sort.SliceStable(procs, func(i, j int) bool {
		a, b := procs[i], procs[j]
		switch key {
		case ByMem:
			if a.RSS != b.RSS {
				return a.RSS > b.RSS
			}
		case ByCPU:
			if a.CPU != b.CPU {
				return a.CPU > b.CPU
			}
		}
		return a.PID < b.PID
	})
}

func FilterProcesses(procs []Process, filter string) []Process {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return procs
	}
	var out []Process
	for _, p := range procs {
		if strings.Contains(strings.ToLower(p.Command), filter) || strings.Contains(strings.ToLower(p.User), filter) || strconv.Itoa(p.PID) == filter {
			out = append(out, p)
		}
	}
	return out
}

func Signal(ctx context.Context, run Runner, p Process, sig syscall.Signal) error {
	if p.PID <= 1 || p.PID == os.Getpid() || p.PID == os.Getppid() {
		return errors.New("refusing to signal init, devcli or its parent")
	}
	out, err := run(ctx, "ps", "-p", strconv.Itoa(p.PID), "-o", "comm=")
	current := strings.TrimSpace(out)
	if err != nil || current == "" {
		return errors.New("process has already exited")
	}
	if current != p.Command {
		return errors.New("pid now belongs to " + current + "; refresh and try again")
	}
	return syscall.Kill(p.PID, sig)
}
