package jvm

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/diegopacheco/dev-cli/internal/sys"
)

type Language string

const (
	Java    Language = "Java"
	Scala   Language = "Scala"
	Kotlin  Language = "Kotlin"
	Clojure Language = "Clojure"
)

type Process struct {
	PID      int
	Language Language
	Main     string
	Command  string
}

var markers = []struct {
	lang    Language
	needles []string
}{
	{Clojure, []string{"clojure.main", "/clojure-", "clojure/clojure", ".lein", "leiningen", "clojure/tools"}},
	{Scala, []string{"scala-library", "scala3-library", "scala.tools", "dotty", "sbt-launch", "coursier", "scala-cli"}},
	{Kotlin, []string{"kotlin-stdlib", "kotlinc", "kotlin-compiler", "kotlincompile", ".main.kts"}},
}

func Detect(command string) Language {
	lower := strings.ToLower(command)
	for _, m := range markers {
		for _, n := range m.needles {
			if strings.Contains(lower, n) {
				return m.lang
			}
		}
	}
	return Java
}

var optionsWithValue = map[string]bool{
	"-cp": true, "-classpath": true, "--class-path": true, "-p": true, "--module-path": true,
	"--add-opens": true, "--add-exports": true, "--add-modules": true, "-m": true, "--module": true,
}

func MainName(args []string) string {
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-jar" && i+1 < len(args):
			return filepath.Base(args[i+1])
		case optionsWithValue[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a
		}
	}
	return "?"
}

func ParseProcesses(out string) []Process {
	var list []Process
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || filepath.Base(f[1]) != "java" {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		command := strings.Join(f[1:], " ")
		list = append(list, Process{PID: pid, Language: Detect(command), Main: MainName(f[1:]), Command: command})
	}
	return list
}

func Discover(ctx context.Context, run sys.Runner) ([]Process, error) {
	out, err := run(ctx, "ps", "-axww", "-o", "pid=,command=")
	if err != nil {
		return nil, err
	}
	return ParseProcesses(out), nil
}

type Line struct {
	Text string
	Lock bool
}

type Thread struct {
	Name       string
	Daemon     bool
	CPU        string
	State      string
	Detail     string
	Frames     []Line
	Deadlocked bool
}

type Dump struct {
	Header    string
	Threads   []Thread
	Deadlocks []string
}

var (
	headerPattern = regexp.MustCompile(`^"(.*)"\s(.*)$`)
	statePattern  = regexp.MustCompile(`java\.lang\.Thread\.State:\s+(\S+)\s*(.*)$`)
	cpuPattern    = regexp.MustCompile(`cpu=(\S+)`)
)

func ParseDump(out string) Dump {
	var d Dump
	var cur *Thread
	flush := func() {
		if cur != nil {
			if cur.State == "" {
				cur.State = "VM"
			}
			d.Threads = append(d.Threads, *cur)
			cur = nil
		}
	}
	lines := strings.Split(out, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \r")
		if strings.HasPrefix(line, "Full thread dump") {
			d.Header = line
			continue
		}
		if strings.HasPrefix(line, "Found one Java-level deadlock") || strings.HasPrefix(line, "Found a total of") {
			flush()
			inReport := false
			for ; i < len(lines); i++ {
				l := strings.TrimRight(lines[i], " \r")
				switch {
				case strings.HasPrefix(l, "Found one Java-level deadlock") || strings.HasPrefix(l, "Found a total of"):
					inReport = true
					d.Deadlocks = append(d.Deadlocks, l)
				case strings.HasPrefix(l, "Java stack information"):
					inReport = false
				case strings.HasPrefix(l, "Found ") && strings.Contains(l, " deadlock") && strings.HasSuffix(l, "."):
					d.Deadlocks = append(d.Deadlocks, l)
				case inReport:
					d.Deadlocks = append(d.Deadlocks, l)
				}
			}
			break
		}
		if g := headerPattern.FindStringSubmatch(line); g != nil && (strings.Contains(g[2], "prio=") || strings.Contains(g[2], "nid=")) {
			flush()
			cur = &Thread{Name: g[1], Daemon: strings.Contains(g[2], " daemon ")}
			if c := cpuPattern.FindStringSubmatch(g[2]); c != nil {
				cur.CPU = c[1]
			}
			continue
		}
		if cur == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case statePattern.MatchString(trimmed):
			g := statePattern.FindStringSubmatch(trimmed)
			cur.State, cur.Detail = g[1], g[2]
		case strings.HasPrefix(trimmed, "at "):
			cur.Frames = append(cur.Frames, Line{Text: strings.TrimPrefix(trimmed, "at ")})
		case strings.HasPrefix(trimmed, "- ") && len(cur.Frames) > 0:
			cur.Frames = append(cur.Frames, Line{Text: strings.TrimPrefix(trimmed, "- "), Lock: true})
		case strings.HasPrefix(trimmed, "Locked ownable synchronizers"):
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "- ") {
				i++
			}
		}
	}
	flush()
	names := map[string]bool{}
	for _, l := range d.Deadlocks {
		if strings.HasPrefix(l, `"`) && strings.HasSuffix(l, `":`) {
			names[strings.TrimSuffix(strings.TrimPrefix(l, `"`), `":`)] = true
		}
	}
	for i := range d.Threads {
		d.Threads[i].Deadlocked = names[d.Threads[i].Name]
	}
	return d
}

func (d Dump) Counts() map[string]int {
	c := map[string]int{}
	for _, t := range d.Threads {
		c[t.State]++
	}
	return c
}

func ThreadDump(ctx context.Context, run sys.Runner, pid int) (Dump, error) {
	out, err := run(ctx, "jcmd", strconv.Itoa(pid), "Thread.print", "-l")
	if err != nil {
		return Dump{}, err
	}
	return ParseDump(out), nil
}

func IsRuntimeFrame(frame string) bool {
	for _, p := range []string{"java.", "javax.", "jdk.", "sun.", "com.sun.", "clojure.lang.", "clojure.core", "clojure.main", "scala.", "kotlin.", "kotlinx."} {
		if strings.HasPrefix(frame, p) {
			return true
		}
	}
	return false
}

var (
	clojureMunge = strings.NewReplacer("_QMARK_", "?", "_BANG_", "!", "_GT_", ">", "_LT_", "<", "_EQ_", "=", "_STAR_", "*", "_PLUS_", "+", "_SLASH_", "/", "_", "-")
	scalaAnonfun = regexp.MustCompile(`\$anonfun\$([^$]+)`)
	anonClojure  = regexp.MustCompile(`^fn--?\d+$`)
	lambdaSuffix = regexp.MustCompile(`\$\d+$`)
)

func Demangle(lang Language, frame string) string {
	method, location, _ := strings.Cut(frame, "(")
	if location != "" {
		location = " (" + location
	}
	dot := strings.LastIndex(method, ".")
	if dot < 0 {
		return frame
	}
	class, fn := method[:dot], method[dot+1:]
	switch lang {
	case Clojure:
		ns, name, ok := strings.Cut(class, "$")
		if !ok || !strings.HasPrefix(fn, "invoke") && fn != "doInvoke" && fn != "applyTo" {
			return frame
		}
		parts := strings.Split(name, "$")
		for i, p := range parts {
			p = clojureMunge.Replace(p)
			if anonClojure.MatchString(p) || strings.HasPrefix(p, "fn--") {
				p = "fn"
			}
			parts[i] = p
		}
		return clojureMunge.Replace(ns) + "/" + strings.Join(parts, "/") + location
	case Scala:
		if g := scalaAnonfun.FindStringSubmatch(fn); g != nil {
			return strings.TrimSuffix(class, "$") + "." + g[1] + " (lambda)" + location
		}
		if base, _, ok := strings.Cut(class, "$$anon"); ok {
			return base + "." + fn + " (anon)" + location
		}
		if strings.HasSuffix(class, "$") {
			return strings.TrimSuffix(class, "$") + "." + fn + location
		}
	case Kotlin:
		if (fn == "invokeSuspend" || fn == "invoke") && strings.Contains(class, "$") {
			outer, inner, _ := strings.Cut(class, "$")
			inner = lambdaSuffix.ReplaceAllString(inner, "")
			kind := " (lambda)"
			if fn == "invokeSuspend" {
				kind = " (coroutine)"
			}
			return outer + "." + inner + kind + location
		}
	}
	return frame
}
