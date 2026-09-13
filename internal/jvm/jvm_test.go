package jvm

import (
	"strings"
	"testing"
)

func TestDetectLanguageFromCommandLine(t *testing.T) {
	cases := map[string]Language{
		"/usr/bin/java -classpath src:/m2/org/clojure/clojure/1.12.4/clojure-1.12.4.jar clojure.main -e (x)": Clojure,
		"java -cp app.jar:/ivy/scala-library-2.13.12.jar app.Main":                                           Scala,
		"java -cp lib/scala3-library_3-3.4.0.jar app.main":                                                   Scala,
		"java -cp build/libs/app.jar:/gradle/kotlin-stdlib-2.0.0.jar app.MainKt":                             Kotlin,
		"java -Xmx1g -jar service.jar":                                                                       Java,
	}
	for cmd, want := range cases {
		if got := Detect(cmd); got != want {
			t.Errorf("%s: got %s want %s", cmd, got, want)
		}
	}
}

func TestClojureWinsOverScalaMarkersInSharedClasspaths(t *testing.T) {
	if Detect("java -cp scala-library.jar:clojure-1.12.jar clojure.main") != Clojure {
		t.Fatal("clojure.main is the entry point, the scala jar is only a dependency")
	}
}

func TestMainNameSkipsOptionValues(t *testing.T) {
	if got := MainName(strings.Fields("java -Xmx1g -cp a.jar:b.jar --add-opens java.base/java.lang=ALL-UNNAMED com.acme.Server arg")); got != "com.acme.Server" {
		t.Fatalf("got %s", got)
	}
	if got := MainName(strings.Fields("java -Dx=1 -jar /opt/app/service.jar")); got != "service.jar" {
		t.Fatalf("got %s", got)
	}
}

func TestParseProcessesOnlyKeepsJava(t *testing.T) {
	out := "42331 java sample/java25/src/DevcliJvm.java\n42332 /jdk/bin/java -cp clojure-1.12.4.jar clojure.main -e x\n501 /bin/zsh -l\n"
	p := ParseProcesses(out)
	if len(p) != 2 || p[0].Main != "sample/java25/src/DevcliJvm.java" || p[1].Language != Clojure || p[1].Main != "clojure.main" {
		t.Fatalf("got %+v", p)
	}
}

const dump = `42331:
2026-09-12 15:13:37
Full thread dump OpenJDK 64-Bit Server VM (25.0.2+10-LTS mixed mode, sharing):

"main" #3 [8963] prio=5 os_prio=31 cpu=231.83ms elapsed=0.53s tid=0x1 nid=8963 in Object.wait()  [0x2]
   java.lang.Thread.State: WAITING (on object monitor)
	at java.lang.Object.wait0(java.base@25.0.2/Native Method)
	- waiting on <0x0000007080001f28> (a java.lang.Thread)
	at DevcliJvm.main(DevcliJvm.java:10)

   Locked ownable synchronizers:
	- None

"order-worker" #30 [1] prio=5 os_prio=31 cpu=1.00ms elapsed=5s tid=0x3 nid=1 waiting for monitor entry  [0x4]
   java.lang.Thread.State: BLOCKED (on object monitor)
	at DevcliJvm.lockBoth(DevcliJvm.java:22)
	- waiting to lock <0xa> (a java.lang.Object)

"Service Thread" #18 [25091] daemon prio=9 os_prio=31 cpu=0.03ms elapsed=0.50s tid=0x5 nid=25091 runnable  [0x0]
   java.lang.Thread.State: RUNNABLE

"VM Thread" os_prio=31 cpu=6.21ms elapsed=7.03s tid=0x9 nid=19459 runnable

JNI global refs: 6, weak refs: 0

Found one Java-level deadlock:
=============================
"order-worker":
  waiting to lock monitor 0x0000000bf1104d20 (object 0x000000707ff66ca8, a java.lang.Object),
  which is held by "stock-worker"

Java stack information for the threads listed above:
===================================================
"order-worker":
	at DevcliJvm.lockBoth(DevcliJvm.java:22)

Found 1 deadlock.
`

func TestParseDumpStatesFramesAndLocks(t *testing.T) {
	d := ParseDump(dump)
	if len(d.Threads) != 4 {
		t.Fatalf("stack information after the deadlock report must not create duplicate threads, got %d", len(d.Threads))
	}
	main := d.Threads[0]
	if main.Name != "main" || main.State != "WAITING" || main.Detail != "(on object monitor)" || main.CPU != "231.83ms" {
		t.Fatalf("got %+v", main)
	}
	if len(main.Frames) != 3 || !main.Frames[1].Lock || main.Frames[2].Text != "DevcliJvm.main(DevcliJvm.java:10)" {
		t.Fatalf("frames and lock lines must stay in order, got %+v", main.Frames)
	}
	if !d.Threads[2].Daemon || d.Threads[3].State != "VM" {
		t.Fatalf("daemon flag and VM threads, got %+v", d.Threads[2:])
	}
	counts := d.Counts()
	if counts["BLOCKED"] != 1 || counts["RUNNABLE"] != 1 {
		t.Fatalf("got %v", counts)
	}
}

func TestParseDumpMarksDeadlockedThreads(t *testing.T) {
	d := ParseDump(dump)
	if !d.Threads[1].Deadlocked || d.Threads[0].Deadlocked {
		t.Fatal("only threads named in the deadlock report are deadlocked")
	}
	if len(d.Deadlocks) == 0 || !strings.Contains(strings.Join(d.Deadlocks, "\n"), "which is held by \"stock-worker\"") || d.Deadlocks[len(d.Deadlocks)-1] != "Found 1 deadlock." {
		t.Fatalf("got %q", d.Deadlocks)
	}
}

func TestDemangleShowsSourceLevelNames(t *testing.T) {
	cases := []struct {
		lang  Language
		raw   string
		shown string
	}{
		{Clojure, "my_app.core$handle_request.invokeStatic(core.clj:12)", "my-app.core/handle-request (core.clj:12)"},
		{Clojure, "my_app.core$valid_QMARK_.invoke(core.clj:3)", "my-app.core/valid? (core.clj:3)"},
		{Clojure, "my_app.core$start$fn__1234.invoke(core.clj:40)", "my-app.core/start/fn (core.clj:40)"},
		{Scala, "app.Main$.$anonfun$run$1(Main.scala:5)", "app.Main.run (lambda) (Main.scala:5)"},
		{Scala, "app.Main$.main(Main.scala:3)", "app.Main.main (Main.scala:3)"},
		{Kotlin, "app.WorkerKt$main$1.invokeSuspend(Worker.kt:10)", "app.WorkerKt.main (coroutine) (Worker.kt:10)"},
		{Java, "app.Main$Inner.run(Main.java:1)", "app.Main$Inner.run(Main.java:1)"},
		{Clojure, "java.lang.Thread.sleep(java.base@25/Thread.java:540)", "java.lang.Thread.sleep(java.base@25/Thread.java:540)"},
	}
	for _, c := range cases {
		if got := Demangle(c.lang, c.raw); got != c.shown {
			t.Errorf("%s %s: got %q want %q", c.lang, c.raw, got, c.shown)
		}
	}
}

func TestRuntimeFramesAreRecognized(t *testing.T) {
	if !IsRuntimeFrame("clojure.lang.Compiler.eval(Compiler.java:1)") || IsRuntimeFrame("devcli.sample$worker_loop.invoke") {
		t.Fatal("application frames must not be dimmed")
	}
}

const twoDeadlocks = `"ledger-writer" #54 [1] daemon prio=5 os_prio=31 cpu=0.01ms elapsed=6s tid=0x1 nid=1 waiting for monitor entry  [0x1]
   java.lang.Thread.State: BLOCKED (on object monitor)
	at Deadlocks.monitors(Deadlocks.java:20)

"payment-refund" #57 [2] daemon prio=5 os_prio=31 cpu=0.01ms elapsed=6s tid=0x2 nid=2 waiting on condition  [0x2]
   java.lang.Thread.State: WAITING (parking)
	at jdk.internal.misc.Unsafe.park(java.base@25.0.2/Native Method)
	- parking to wait for  <0x00000070350002e8> (a java.util.concurrent.locks.ReentrantLock$NonfairSync)
	at Deadlocks.locks(Deadlocks.java:29)

   Locked ownable synchronizers:
	- <0x000000703514f978> (a java.util.concurrent.locks.ReentrantLock$NonfairSync)

Found one Java-level deadlock:
=============================
"ledger-writer":
  waiting to lock monitor 0x000000089b592ae0 (object 0x000000703503d2c0, a java.lang.Object),
  which is held by "inventory-writer"

Java stack information for the threads listed above:
===================================================
"ledger-writer":
	at Deadlocks.monitors(Deadlocks.java:20)

Found one Java-level deadlock:
=============================
"payment-refund":
  waiting for ownable synchronizer 0x00000070350002e8, (a java.util.concurrent.locks.ReentrantLock$NonfairSync),
  which is held by "payment-capture"

Java stack information for the threads listed above:
===================================================
"payment-refund":
	at jdk.internal.misc.Unsafe.park(java.base@25.0.2/Native Method)

Found 2 deadlocks.
`

func TestParseDumpFindsEveryDeadlockNotOnlyTheFirst(t *testing.T) {
	d := ParseDump(twoDeadlocks)
	if len(d.Threads) != 2 {
		t.Fatalf("got %d threads", len(d.Threads))
	}
	if !d.Threads[0].Deadlocked || !d.Threads[1].Deadlocked {
		t.Fatal("a ReentrantLock deadlock reported after a monitor deadlock must still mark its threads, or users miss it")
	}
	report := strings.Join(d.Deadlocks, "\n")
	if !strings.Contains(report, "ownable synchronizer") || !strings.HasSuffix(report, "Found 2 deadlocks.") {
		t.Fatalf("both deadlock reports and the total must be kept, got %q", report)
	}
}
