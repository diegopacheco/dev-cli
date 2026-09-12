//go:build integration

package jvm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/diegopacheco/dev-cli/internal/sys"
)

func TestIntegrationDeadlockedSampleJVM(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list, err := Discover(ctx, sys.Run)
	if err != nil {
		t.Fatal(err)
	}
	var target, clojure *Process
	for i, p := range list {
		if strings.Contains(p.Main, "DevcliJvm") {
			target = &list[i]
		}
		if p.Language == Clojure && strings.Contains(p.Command, "devcli.sample") {
			clojure = &list[i]
		}
	}
	if target == nil || clojure == nil {
		t.Fatalf("sample JVMs not running; run scripts/start-all.sh, found %+v", list)
	}
	d, err := ThreadDump(ctx, sys.Run, target.PID)
	if err != nil {
		t.Fatal(err)
	}
	deadlocked := 0
	for _, th := range d.Threads {
		if th.Deadlocked {
			deadlocked++
		}
	}
	if deadlocked != 2 {
		t.Fatalf("order-worker and stock-worker deadlock by design, found %d deadlocked threads", deadlocked)
	}
	cd, err := ThreadDump(ctx, sys.Run, clojure.PID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, th := range cd.Threads {
		for _, f := range th.Frames {
			if strings.HasPrefix(Demangle(Clojure, f.Text), "devcli.sample/worker-loop") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the Clojure sample frame must demangle to devcli.sample/worker-loop")
	}
}
