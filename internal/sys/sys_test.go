package sys

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

const topSample = `Processes: 1133 total, 4 running, 1129 sleeping, 8392 threads
2026/09/12 15:00:52
Load Avg: 3.81, 4.27, 4.77
CPU usage: 16.4% user, 6.58% sys, 77.36% idle
PhysMem: 102G used (5484M wired, 1113M compressor), 25G unused.
Networks: packets: 130090421/115G in, 72667046/35G out.
Disks: 113661994/860G read, 49444237/885G written.
`

func TestParseTopReadsCPUMemoryAndCounts(t *testing.T) {
	m := ParseTop(topSample)
	if m.CPU < 22.63 || m.CPU > 22.65 || m.User != 16.4 || m.System != 6.58 {
		t.Fatalf("cpu must be 100-idle so user+sys+nice all count, got %+v", m)
	}
	if m.MemUsed != 102<<30 || m.MemWired != 5484<<20 || m.MemCompressed != 1113<<20 || m.MemFree != 25<<30 {
		t.Fatalf("memory wrong: %+v", m)
	}
	if m.Processes != 1133 || m.Threads != 8392 || m.Load != "3.81, 4.27, 4.77" {
		t.Fatalf("header wrong: %+v", m)
	}
}

func TestTopStreamSkipsSinceBootSample(t *testing.T) {
	first := strings.Replace(topSample, "77.36% idle", "10.00% idle", 1)
	second := strings.Replace(topSample, "77.36% idle", "50.00% idle", 1)
	third := strings.Replace(topSample, "77.36% idle", "60.00% idle", 1)
	var got []float64
	ReadTopSamples(strings.NewReader(first+second+third), func(m Metrics) { got = append(got, m.CPU) })
	if len(got) != 1 || got[0] != 50 {
		t.Fatalf("the first top sample is a since-boot average and must not reach the graph, got %v", got)
	}
}

func TestParseNetstatSumsLinkRowsExcludingLoopback(t *testing.T) {
	out := `Name       Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0        16384 <Link#1>                      31783741     0 13020380311 31783741     0 13020380311     0
en0        1500  <Link#11>   aa:bb:cc:dd:ee:ff  100     0       5000      200     0       7000     0
en0        1500  192.168.1     192.168.1.5        100     -       5000      200     -       7000     -
utun0      1380  <Link#15>                           10     0        300       20     0        400     0
`
	in, o := ParseNetstat(out)
	if in != 5300 || o != 7400 {
		t.Fatalf("address rows repeat link counters and loopback is not network traffic, got %d %d", in, o)
	}
}

func TestParseIORegSumsDisks(t *testing.T) {
	out := `"Statistics" = {"Bytes (Read)"=10,"Bytes (Write)"=20}
"Statistics" = {"Bytes (Read)"=5,"Bytes (Write)"=7}`
	if r, w := ParseIOReg(out); r != 15 || w != 27 {
		t.Fatalf("got %d %d", r, w)
	}
}

func TestParseDFKeepsDataAndExternalVolumes(t *testing.T) {
	out := `Filesystem     1024-blocks       Used  Available Capacity  iused       ifree %iused  Mounted on
/dev/disk3s1s1  3902665360   12341188 2101442048     1%   458732  4293353235    0%   /
/dev/disk3s6    3902665360         20 2101442048     1%        0 21014420480    0%   /System/Volumes/VM
/dev/disk3s5    3902665360 1778300576 2101442048    46% 11733901 21014420480    0%   /System/Volumes/Data
/dev/disk5s1      17231872   16741356     446332    98%   607237     4463320   12%   /Library/Developer/CoreSimulator/Volumes/iOS_23C54
/dev/disk8s1      1000       500     500    50%   1     1   0%   /Volumes/My Backup
`
	v := ParseDF(out)
	if len(v) != 2 || v[0].Mount != "/System/Volumes/Data" || v[1].Mount != "/Volumes/My Backup" || v[0].Used != 1778300576<<10 {
		t.Fatalf("the sealed system snapshot at / reports 1%% and would hide a full disk, got %+v", v)
	}
}

func TestRateIgnoresCounterReset(t *testing.T) {
	if Rate(100, 50, time.Second) != 0 || Rate(0, 2048, 2*time.Second) != 1024 {
		t.Fatal("a reset counter must not draw a negative spike")
	}
}

func TestParsePSKeepsCommandsWithSpaces(t *testing.T) {
	p := ParsePS("  812     1 diego   12.5  1.2  204800   01:02:03 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome\n")
	if len(p) != 1 || p[0].Name() != "Google Chrome" || p[0].RSS != 204800<<10 || p[0].CPU != 12.5 {
		t.Fatalf("got %+v", p)
	}
}

func TestFilterAndSortProcesses(t *testing.T) {
	procs := []Process{{PID: 3, CPU: 1, RSS: 9, Command: "/bin/zsh", User: "root"}, {PID: 2, CPU: 5, RSS: 1, Command: "/usr/bin/java"}, {PID: 10, CPU: 5, RSS: 3, Command: "node"}}
	SortProcesses(procs, ByCPU)
	if procs[0].PID != 2 || procs[1].PID != 10 {
		t.Fatalf("ties must break by pid so rows do not jump between refreshes, got %+v", procs)
	}
	if f := FilterProcesses(procs, "JAVA"); len(f) != 1 || f[0].PID != 2 {
		t.Fatalf("filter is case-insensitive over command, got %+v", f)
	}
	if f := FilterProcesses(procs, "10"); len(f) != 1 || f[0].PID != 10 {
		t.Fatalf("pid filter must match exact pid, got %+v", f)
	}
}

func TestSignalRefusesDangerousTargets(t *testing.T) {
	run := func(ctx context.Context, name string, args ...string) (string, error) { return "/bin/other\n", nil }
	for _, pid := range []int{0, 1, os.Getpid(), os.Getppid()} {
		if err := Signal(context.Background(), run, Process{PID: pid, Command: "/bin/other"}, syscall.SIGTERM); err == nil {
			t.Fatalf("pid %d must be refused", pid)
		}
	}
	err := Signal(context.Background(), run, Process{PID: 99999, Command: "/bin/listed"}, syscall.SIGTERM)
	if err == nil || !strings.Contains(err.Error(), "now belongs to") {
		t.Fatalf("a reused pid must not be killed, got %v", err)
	}
}

func TestParsePodmanContainers(t *testing.T) {
	out := `[{"Id":"1baed1281cf931f3a49b","Names":["devcli-redis"],"Image":"docker.io/library/redis:8","State":"running","Status":"Up 2 minutes","Ports":[{"host_ip":"","container_port":6379,"host_port":6380,"protocol":"tcp"}]}]`
	c, err := ParsePodman(out)
	if err != nil || len(c) != 1 || c[0].ID != "1baed1281cf9" || c[0].Name != "devcli-redis" || c[0].Ports != "6380->6379/tcp" {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestParseDockerContainers(t *testing.T) {
	out := `{"ID":"abc","Names":"web","Image":"nginx","State":"exited","Status":"Exited (0)","Ports":""}` + "\n"
	c, err := ParseDocker(out)
	if err != nil || len(c) != 1 || c[0].State != "exited" {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestRemoveForcesSoRunningContainersCanBeRemoved(t *testing.T) {
	if strings.Join(ActionArgs(Remove, "abc"), " ") != "rm -f abc" || ActionArgs("bogus", "abc") != nil {
		t.Fatal("unexpected action args")
	}
}

func TestParseSwapAndSize(t *testing.T) {
	used, total := ParseSwap("total = 2048.00M  used = 512.00M  free = 1536.00M  (encrypted)")
	if used != 512<<20 || total != 2048<<20 || ParseSize("1.5G") != 1610612736 {
		t.Fatalf("got %d %d", used, total)
	}
}
