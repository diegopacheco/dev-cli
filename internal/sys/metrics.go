package sys

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Metrics struct {
	Time          time.Time
	CPU           float64
	User          float64
	System        float64
	MemUsed       int64
	MemFree       int64
	MemWired      int64
	MemCompressed int64
	Processes     int
	Threads       int
	Load          string
	NetIn         int64
	NetOut        int64
	DiskRead      int64
	DiskWrite     int64
}

type Static struct {
	Cores     int
	MemTotal  int64
	Model     string
	BootTime  time.Time
	SwapUsed  int64
	SwapTotal int64
}

type Volume struct {
	Mount string
	Total int64
	Used  int64
	Free  int64
}

var (
	cpuPattern     = regexp.MustCompile(`CPU usage:\s+([\d.]+)% user,\s+([\d.]+)% sys,\s+([\d.]+)% idle`)
	memPattern     = regexp.MustCompile(`PhysMem:\s+(\S+) used \((\S+) wired(?:, (\S+) compressor)?\),\s+(\S+) unused`)
	procPattern    = regexp.MustCompile(`Processes:\s+(\d+) total.*?(\d+) threads`)
	diskReadBytes  = regexp.MustCompile(`"Bytes \(Read\)"=(\d+)`)
	diskWriteBytes = regexp.MustCompile(`"Bytes \(Write\)"=(\d+)`)
	swapPattern    = regexp.MustCompile(`total = (\S+)\s+used = (\S+)`)
	bootPattern    = regexp.MustCompile(`sec = (\d+)`)
)

func ParseSize(s string) int64 {
	s = strings.TrimRight(strings.TrimSpace(s), ".,")
	if s == "" {
		return 0
	}
	factor := 1.0
	switch s[len(s)-1] {
	case 'B':
		s = s[:len(s)-1]
	case 'K':
		factor, s = 1<<10, s[:len(s)-1]
	case 'M':
		factor, s = 1<<20, s[:len(s)-1]
	case 'G':
		factor, s = 1<<30, s[:len(s)-1]
	case 'T':
		factor, s = 1<<40, s[:len(s)-1]
	}
	n, _ := strconv.ParseFloat(s, 64)
	return int64(n * factor)
}

func ParseTop(sample string) Metrics {
	var m Metrics
	for _, line := range strings.Split(sample, "\n") {
		if g := cpuPattern.FindStringSubmatch(line); g != nil {
			m.User, _ = strconv.ParseFloat(g[1], 64)
			m.System, _ = strconv.ParseFloat(g[2], 64)
			idle, _ := strconv.ParseFloat(g[3], 64)
			m.CPU = max(0, min(100, 100-idle))
		}
		if g := memPattern.FindStringSubmatch(line); g != nil {
			m.MemUsed, m.MemWired, m.MemCompressed, m.MemFree = ParseSize(g[1]), ParseSize(g[2]), ParseSize(g[3]), ParseSize(g[4])
		}
		if g := procPattern.FindStringSubmatch(line); g != nil {
			m.Processes, _ = strconv.Atoi(g[1])
			m.Threads, _ = strconv.Atoi(g[2])
		}
		if rest, ok := strings.CutPrefix(line, "Load Avg:"); ok {
			m.Load = strings.TrimSpace(rest)
		}
	}
	return m
}

func ParseNetstat(out string) (in, outBytes int64) {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || !strings.HasPrefix(f[2], "<Link#") || strings.HasPrefix(f[0], "lo") {
			continue
		}
		n := len(f)
		i, _ := strconv.ParseInt(f[n-5], 10, 64)
		o, _ := strconv.ParseInt(f[n-2], 10, 64)
		in += i
		outBytes += o
	}
	return in, outBytes
}

func ParseIOReg(out string) (read, write int64) {
	for _, g := range diskReadBytes.FindAllStringSubmatch(out, -1) {
		n, _ := strconv.ParseInt(g[1], 10, 64)
		read += n
	}
	for _, g := range diskWriteBytes.FindAllStringSubmatch(out, -1) {
		n, _ := strconv.ParseInt(g[1], 10, 64)
		write += n
	}
	return read, write
}

func ParseDF(out string) []Volume {
	var all, keep []Volume
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 9 || !strings.HasPrefix(f[0], "/dev/") {
			continue
		}
		total, _ := strconv.ParseInt(f[1], 10, 64)
		used, _ := strconv.ParseInt(f[2], 10, 64)
		free, _ := strconv.ParseInt(f[3], 10, 64)
		v := Volume{Mount: strings.Join(f[8:], " "), Total: total << 10, Used: used << 10, Free: free << 10}
		all = append(all, v)
		if v.Mount == "/System/Volumes/Data" || strings.HasPrefix(v.Mount, "/Volumes/") {
			keep = append(keep, v)
		}
	}
	if len(keep) == 0 {
		for _, v := range all {
			if v.Mount == "/" {
				keep = append(keep, v)
			}
		}
	}
	return keep
}

func ParseSwap(out string) (used, total int64) {
	if g := swapPattern.FindStringSubmatch(out); g != nil {
		return ParseSize(g[2]), ParseSize(g[1])
	}
	return 0, 0
}

func Rate(prev, cur int64, elapsed time.Duration) float64 {
	if elapsed <= 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / elapsed.Seconds()
}

func LoadStatic(ctx context.Context, run Runner) Static {
	var s Static
	if out, err := run(ctx, "sysctl", "-n", "hw.ncpu"); err == nil {
		s.Cores, _ = strconv.Atoi(strings.TrimSpace(out))
	}
	if out, err := run(ctx, "sysctl", "-n", "hw.memsize"); err == nil {
		s.MemTotal, _ = strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	}
	if out, err := run(ctx, "sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
		s.Model = strings.TrimSpace(out)
	}
	if out, err := run(ctx, "sysctl", "-n", "kern.boottime"); err == nil {
		if g := bootPattern.FindStringSubmatch(out); g != nil {
			sec, _ := strconv.ParseInt(g[1], 10, 64)
			s.BootTime = time.Unix(sec, 0)
		}
	}
	if out, err := run(ctx, "sysctl", "-n", "vm.swapusage"); err == nil {
		s.SwapUsed, s.SwapTotal = ParseSwap(out)
	}
	return s
}

func Volumes(ctx context.Context, run Runner) []Volume {
	out, err := run(ctx, "df", "-kl")
	if err != nil && out == "" {
		return nil
	}
	return ParseDF(out)
}

func counters(ctx context.Context, run Runner, m *Metrics) {
	if out, err := run(ctx, "netstat", "-ib"); err == nil {
		m.NetIn, m.NetOut = ParseNetstat(out)
	}
	if out, err := run(ctx, "ioreg", "-c", "IOBlockStorageDriver", "-r", "-k", "Statistics"); err == nil {
		m.DiskRead, m.DiskWrite = ParseIOReg(out)
	}
}

func ReadTopSamples(r io.Reader, emit func(Metrics)) error {
	scanner := bufio.NewScanner(r)
	var sample strings.Builder
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Processes:") && sample.Len() > 0 {
			if !first {
				emit(ParseTop(sample.String()))
			}
			first = false
			sample.Reset()
		}
		sample.WriteString(line)
		sample.WriteByte('\n')
	}
	return scanner.Err()
}

func StreamMetrics(ctx context.Context, run Runner, emit func(Metrics)) error {
	cmd := exec.CommandContext(ctx, "top", "-l", "0", "-s", "1", "-n", "0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	readErr := ReadTopSamples(out, func(m Metrics) {
		m.Time = time.Now()
		counters(ctx, run, &m)
		emit(m)
	})
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		return waitErr
	}
	return errors.New("top exited")
}
