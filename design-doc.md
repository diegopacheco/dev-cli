# devcli — Design Doc

Status: **built**. Build order is in §11; §12 records the decisions that changed while building.

## 1. What this is

`devcli` is one Go terminal binary that replaces the pile of tools a developer keeps open on a laptop:
database shells, `redis-cli`, Loki and Grafana HTTP pokes, `jcmd Thread.print`, `btop`, `ps | grep | kill`
and `podman ps / exec / rm`. Everything lives in one full-screen TUI with tabs, neon colors, mouse support
and the same editor experience in every query console.

| # | Tab | What it does |
|---|---|---|
| 1 | Dashboard | btop-style live CPU, memory, disk and network panels plus top processes |
| 2 | Processes | Full process list, text filter, sort, SIGTERM / SIGKILL with confirmation |
| 3 | Containers | Podman (or Docker) containers: stop, shell in, kill, remove |
| 4 | Threads | Discover local JVMs, tag them Java / Scala / Kotlin / Clojure, colored thread dumps |
| 5 | MySQL | SQL console |
| 6 | Postgres | SQL console |
| 7 | SQLite | SQL console over a database file |
| 8 | Cassandra | CQL console |
| 9 | Redis | Redis command REPL |
| 10 | Loki | LogQL REPL |
| 11 | Grafana | Grafana HTTP API REPL |

Every console tab (5–11) shares one editor widget: syntax highlighting, line numbers, as-you-type
auto-complete (static keywords plus live names pulled from the connected server), history, and a results pane
that renders tables or pretty, colored JSON.

## 2. Assumptions

Correct these before code exists; each one changes the build.

1. **macOS first.** The machine is darwin/arm64. System metrics come from `top`, `ps`, `df`, `sysctl` and
   `netstat`, the same approach as the Harbor TUI. Linux collectors are out of scope for v1 (§13).
2. **"ssh into a container" means an interactive shell** through `podman exec -it <id> sh`. Real SSH needs an
   sshd in the image and a published port 22, which almost no dev container has. The TUI suspends, gives the
   terminal to the shell, and resumes when the shell exits.
3. **Podman first, Docker fallback.** `docker` is only a shell alias on this machine, and aliases do not exist
   for `exec.Command`, so the binary looks for `podman` then `docker` on `PATH`.
4. **Connections come from environment variables with local defaults** that match the `podman-compose.yml`
   shipped in this repo. The connection string is also editable inside each tab (`Ctrl-E`).
5. **No read-only guard.** This is a developer tool for local and dev environments; statements run as typed.
6. **Grafana "queries"** means a REPL over the Grafana HTTP API (search, dashboards, datasources, alert rules,
   and `/api/ds/query` to run an expression through any datasource), not a dashboard renderer.
7. **Thread dumps use `jcmd <pid> Thread.print -l`** from the JDK on `PATH`. All four languages run on the JVM,
   so the dump format is identical; the language only changes detection and how frame names are shown.
8. **Redis host port is 6380** in the local compose, because 6379 is already taken on this machine.

## 3. Conflicts and decisions

### 3.1 "Least libraries" vs six protocols — decided: hand-write HTTP and RESP, use drivers for binary protocols

| Target | Decision | Why |
|---|---|---|
| Redis | **Hand-written RESP2 client** (~150 lines) | RESP is a tiny text protocol; a client library adds pools and features we do not use |
| Loki | `net/http` + `encoding/json` | Plain REST |
| Grafana | `net/http` + `encoding/json` | Plain REST |
| MySQL | `github.com/go-sql-driver/mysql` | Wire protocol with auth plugins (caching_sha2) is not worth rewriting |
| Postgres | `github.com/lib/pq` | Zero transitive dependencies, SCRAM auth support |
| SQLite | `github.com/mattn/go-sqlite3` | Zero Go dependencies; needs CGO, which macOS has |
| Cassandra | `github.com/gocql/gocql` | CQL native protocol v4 framing, paging and type codecs |
| TUI | `github.com/rivo/tview` + `github.com/gdamore/tcell/v2` | Layout, tables, modals, mouse, and a simulation screen for tests and captures |
| Syntax highlight | **Hand-written lexer** | Chroma is large; we need seven small keyword sets and one token loop |
| Metrics | **macOS commands** (`top`, `ps`, `df`, `netstat`, `ioreg`) | gopsutil pulls many packages; a streaming `top` gives CPU and memory, byte counters give exact IO and network rates |

### 3.2 Seven consoles vs one editor — decided: one `Console` component, one `Backend` interface

Every console differs only in how it connects, how it executes text, which words it completes and which
language rules it highlights. One component owns editor, completion popup, history, results and status bar.

### 3.3 tview `TextArea` vs highlighting — decided: custom `Editor` primitive

tview's `TextArea` cannot color tokens or draw a gutter. The editor is a `tview.Box` with its own `Draw` and
`InputHandler`: lines as `[][]rune`, a cursor, a gutter with line numbers, lexer-colored cells, and a completion
popup drawn on top.

### 3.4 Per-core CPU vs no CGO syscalls — decided: total CPU history

macOS exposes per-core load through `host_processor_info` (CGO) or `powermetrics` (root). v1 draws total CPU
history with load average and core count, like btop's compact mode.

## 4. Architecture

```mermaid
flowchart LR
  subgraph TUI["internal/ui (tview event loop)"]
    Tabs[Tab bar] --> Dash[Dashboard]
    Tabs --> Procs[Processes]
    Tabs --> Cont[Containers]
    Tabs --> Thr[Threads]
    Tabs --> Con[Console x7]
    Con --> Ed[Editor + completion popup]
    Con --> Res[Results: table / JSON]
  end
  subgraph SYS["internal/sys"]
    Top[top sampler] 
    PS[ps + kill]
    DF[df volumes]
    Pod[podman / docker CLI]
  end
  subgraph JVM["internal/jvm"]
    Disc[discover + language] --> Dump[jcmd Thread.print parser]
  end
  subgraph BE["internal/backend"]
    SQL[database/sql: mysql, postgres, sqlite]
    CQL[gocql]
    RESP[RESP2 client]
    LOKI[Loki HTTP]
    GRAF[Grafana HTTP]
  end
  subgraph SYN["internal/syntax"]
    Lex[lexer] 
    JSON[JSON colorizer]
    Comp[completer]
  end
  Dash --> Top & DF & PS
  Procs --> PS
  Cont --> Pod
  Thr --> JVM
  Con --> BE
  Ed --> SYN
  Res --> SYN
```

Rules:

- Only the tview event loop mutates UI state. Workers send results through `app.QueueUpdateDraw`.
- Every external command runs through one `sys.Run(ctx, name, args...)`: argument arrays, no shell, a timeout,
  and a bounded output buffer.
- Every backend call takes a `context.Context` with a timeout (`10s` default), and `Esc` while a query runs cancels it.

### 4.1 Package layout

```
main.go                     flags, env, starts ui.Run or capture mode
internal/ui/app.go          tab bar, pages, global keys, theme, status line
internal/theme/theme.go     neon palette, heat gradient
internal/ui/draw.go         cell helpers: meters, braille graphs, byte labels
internal/ui/list.go         shared table, filter and info widgets
internal/ui/editor.go       Editor primitive (gutter, highlight, cursor, popup, history)
internal/ui/console.go      Console = connection bar + Editor + results + status
internal/ui/results.go      table and JSON rendering into color-tagged text
internal/ui/dashboard.go    btop-style panels drawn cell by cell (braille graphs, gradient meters)
internal/ui/processes.go    process table, filter, sort, kill confirm
internal/ui/containers.go   container table, stop / shell / kill / remove
internal/ui/threads.go      JVM list + thread dump view with filter
internal/ui/capture.go      render every tab into a simulation screen and export HTML
internal/sys/run.go         command runner
internal/sys/metrics.go     top stream parser, netstat, ioreg, df, sysctl
internal/sys/process.go     ps parser, signal with identity check
internal/sys/container.go   podman / docker JSON parsing and actions
internal/jvm/jvm.go         JVM discovery, language detection, dump parsing, frame demangling
internal/backend/backend.go Backend interface, Result type
internal/backend/sql.go     database/sql backends: mysql, postgres, sqlite
internal/backend/cassandra.go
internal/backend/redis.go   RESP2 client + backend
internal/backend/loki.go
internal/backend/grafana.go
internal/syntax/lexer.go    tokenizer with per-language rules
internal/syntax/json.go     pretty JSON into colored segments
internal/syntax/complete.go prefix completion, ranking
```

### 4.2 Backend interface

```go
type Backend interface {
    Name() string
    Language() syntax.Language
    DefaultTarget() string
    Connect(ctx context.Context, target string) error
    Execute(ctx context.Context, input string) ([]Result, error)
    Words(ctx context.Context) []string
    RunsOnEnter(input string) bool
    Close() error
}

type Result struct {
    Title   string
    Columns []string
    Rows    [][]any
    Logs    []LogLine
    Value   any
    Message string
    Elapsed time.Duration
}
```

- `Columns/Rows` render as a table. `Logs` render as a colored log view (time, level, labels, JSON line). `Value` is any JSON-able value (Redis replies, Loki streams, Grafana JSON).
- `Ctrl-T` toggles **table** and **JSON** views. In JSON view rows become objects; a string cell that parses as
  JSON is embedded as JSON so nested documents are pretty printed and colored.
- `Words` is called after connecting and on `F5`, and feeds the completer.

| Backend | Execute | Words (live) | Enter runs when |
|---|---|---|---|
| MySQL | split on `;` outside quotes, `QueryContext` per statement | `information_schema` tables and columns of the current schema | buffer ends with `;` |
| Postgres | same | `information_schema` tables and columns outside `pg_catalog` | buffer ends with `;` |
| SQLite | same | `sqlite_master` names and `pragma table_info` columns | buffer ends with `;` |
| Cassandra | split on `;`, `session.Query(...).Iter()` with `MapScan`, 1000 row cap | `system_schema.keyspaces/tables/columns` | buffer ends with `;` |
| Redis | tokenize with quotes, send RESP array | command names + up to 1000 keys from `SCAN` | always |
| Loki | `labels`, `values <label>`, `:range 30m`, `:limit 100`, otherwise LogQL via `query_range` | label names and values | always |
| Grafana | `health`, `search [q]`, `dashboard <uid>`, `datasources`, `folders`, `alerts`, `annotations`, `query <ds-uid> <expr>`, `get <path>` | commands, dashboard uids, datasource uids | always |

### 4.3 Syntax highlighting

One lexer, rules per language:

```go
type Language struct {
    Name       string
    Keywords   map[string]bool
    Functions  map[string]bool
    LineComments []string
    BlockComment bool
    CaseFold     bool
    Operators    []string
    WordRunes    string
    Variables    bool
}
```

Token kinds: keyword, function, string, number, comment, operator, identifier, variable (`$x`, `:name`),
label names for LogQL. `WordRunes` lets `user:1` (Redis) and `devcli-logs` (Grafana) stay one word, and `Variables` limits `$x` / `:name` bind variables to SQL and CQL. Each kind maps to one palette color. The lexer is line based and stateless
except for block comments, which is enough for editor-sized buffers.

### 4.4 Completion

- The word under the cursor is the run of `[A-Za-z0-9_.$:-]` before it.
- Candidates = language keywords ∪ backend live words. Matching is case-insensitive prefix first, then
  substring, capped at 8, shorter first.
- Keywords are inserted in the case the user started typing; identifiers keep their real case.
- The popup opens as you type (prefix ≥ 1 char), `Tab` accepts, `Up/Down` move, `Esc` closes.

### 4.5 JSON pretty printing

`syntax.JSON(value)` walks a decoded `any` (with `json.Number` preserved) and emits tview color-tagged text:
keys cyan, strings lime, numbers orange, booleans magenta, null gray, punctuation dim. Objects keep server key
order by decoding with a small ordered-object decoder, so output matches what the server sent.

### 4.6 Dashboard (btop style)

One long-running `top -l 0 -s 1 -n 0` stream. Each sample yields:

| Line | Data |
|---|---|
| `CPU usage: x% user, y% sys, z% idle` | CPU % → 120-sample history |
| `PhysMem: 102G used (5484M wired, 1113M compressor), 25G unused.` | used, wired, compressor, free |
| `Load Avg`, `Processes: … threads` | header |

`top` rounds its `Networks:` and `Disks:` counters to whole gigabytes, which makes per-second rates useless, so each
sample also reads exact byte counters: `netstat -ib` (link rows, loopback excluded) and
`ioreg -c IOBlockStorageDriver -r -k Statistics` (`Bytes (Read)` / `Bytes (Write)`). Rates come from diffing samples.

`df -kl` lists local volumes with a gradient meter each; `sysctl -n hw.ncpu hw.memsize vm.swapusage kern.boottime`
fills totals, swap and uptime. Panels are custom primitives that draw cells directly: CPU and network use
**braille graphs** (2×4 dots per cell) colored by height (green → yellow → red), meters use per-cell gradients.
A top-10 process list refreshes every 2 seconds. If `top` exits the sampler restarts and the last graph stays.

### 4.7 Processes

`ps -axo pid=,ppid=,user=,%cpu=,%mem=,rss=,etime=,comm=` every 2s while the tab is visible. `/` filters by
substring over pid, user and command; `o` cycles sort CPU → MEM → PID. `x` SIGTERM, `K` SIGKILL, both through a
red modal with **Cancel** focused. Before signaling, `ps -p <pid> -o lstart=,comm=` must match what was listed,
and pid ≤ 1, devcli itself and its parent are refused.

### 4.8 Containers

`podman ps -a --format json` every 3s while visible. Keys: `s` stop, `S` start, `Enter`/`e` shell
(`app.Suspend` + `exec -it <id> sh`, falling back from `bash`), `k` kill, `d` remove (`rm -f`). Kill and remove
confirm. State is colored: running lime, exited gray, paused yellow, other red.

### 4.9 Threads

- Discovery: `ps -axww -o pid=,command=` rows whose executable base name is `java`.
- Language detection over the full command line, first match wins:

| Language | Markers |
|---|---|
| Clojure | `clojure.main`, `clojure-`, `.lein`, `leiningen`, `clojure/tools` |
| Scala | `scala-library`, `scala3-library`, `scala.tools`, `dotty`, `sbt-launch`, `coursier`, `scala-cli` |
| Kotlin | `kotlin-stdlib`, `kotlinc`, `kotlin-compiler`, `KotlinCompile`, `.main.kts` |
| Java | anything else |

- Dump: `jcmd <pid> Thread.print -l`, parsed into `Thread{Name, State, Daemon, Frames, Locks}` plus a deadlock
  section when `Found one Java-level deadlock` appears.
- View: summary bar with counts per state (RUNNABLE lime, WAITING yellow, TIMED_WAITING cyan, BLOCKED red),
  deadlocks in a red banner, `/` filters threads by name or frame, `s` cycles state filter, `r` re-dumps.
- Frame demangling per language so frames read like source:

| Language | Raw | Shown |
|---|---|---|
| Clojure | `my_app.core$handle_request.invokeStatic` | `my-app.core/handle-request` |
| Scala | `app.Main$.$anonfun$run$1` | `app.Main.run (lambda)` |
| Kotlin | `app.WorkerKt$main$1.invokeSuspend` | `app.WorkerKt.main (coroutine)` |

JDK frames (`java.`, `jdk.`, `sun.`) are dimmed so application frames stand out.

## 5. UI

### 5.1 Layout

```
 devcli  1 Dashboard  2 Processes  3 Containers  4 Threads  5 MySQL ... 11 Grafana       15:04:05
┌ tab content ─────────────────────────────────────────────────────────────────────────────────┐
│                                                                                              │
└──────────────────────────────────────────────────────────────────────────────────────────────┘
 status: ✔ 12 rows in 3ms                                         Ctrl-N/P tabs  Ctrl-Q quit
```

Console tab:

```
┌ connection ─ postgres://postgres:***@127.0.0.1:5432/devcli ● connected ──────────────────────┐
┌ editor ───────────────────────────┐┌ results ─ table ────────────────────────────────────────┐
│  1 SELECT id, name, profile        ││ id │ name  │ profile                                    │
│  2 FROM users                      ││  1 │ Ana   │ {"lang":"go","level":9}                    │
│  3 WHERE id < 10;▌                 ││                                                          │
│    ┌──────────────┐                ││                                                          │
│    │ WHERE         │                ││                                                          │
└────┴──────────────┴────────────────┘└─────────────────────────────────────────────────────────┘
```

Wide terminals split editor and results side by side; below 120 columns they stack vertically.

### 5.2 Keys

| Scope | Keys |
|---|---|
| Global | `Ctrl-N` / `Ctrl-P` next / previous tab, `F1`–`F11` jump, click a tab, `Ctrl-Q` quit |
| Lists (tabs 1–4) | `↑↓` / `j k` select, `/` filter, `Esc` clear filter, `q` quit, `r` refresh |
| Editor | `Ctrl-R` run buffer, `Enter` newline or run (§4.2), `Tab` accept completion, `↑↓` popup or lines, history on first / last line, `Ctrl-K` clear buffer, `Ctrl-T` table / JSON, `PgUp/PgDn` scroll results, `Ctrl-E` edit connection, `F5` reload completions, `Esc` close popup or cancel running query |

### 5.3 Palette

| Token | Hex | Use |
|---|---|---|
| bg | `#0b0f1a` | background |
| panel | `#11172a` | panel background |
| border | `#2a3355` | idle borders |
| cyan | `#00e5ff` | focus, JSON keys, keywords |
| magenta | `#ff4fd8` | functions, booleans, active tab |
| lime | `#a6ff4d` | strings, running, good |
| orange | `#ffb86c` | numbers, warnings |
| purple | `#bd93f9` | labels, identifiers |
| red | `#ff5c7a` | errors, blocked, kill |
| dim | `#6b7394` | comments, JDK frames, null |

Meters and graphs use a green → yellow → red gradient per cell. The tab bar logo is a cyan → magenta gradient.

## 6. Configuration

| Variable | Default |
|---|---|
| `DEVCLI_MYSQL` | `mysql://root:devcli@127.0.0.1:3306/devcli` |
| `DEVCLI_POSTGRES` | `postgres://postgres:devcli@127.0.0.1:5432/devcli?sslmode=disable` |
| `DEVCLI_SQLITE` | `devcli.db` |
| `DEVCLI_CASSANDRA` | `127.0.0.1:9042/devcli` |
| `DEVCLI_REDIS` | `redis://127.0.0.1:6380/0` |
| `DEVCLI_LOKI` | `http://127.0.0.1:3100` |
| `DEVCLI_GRAFANA` | `http://admin:devcli@127.0.0.1:3000` |
| `DEVCLI_GRAFANA_TOKEN` | empty; when set it is sent as a Bearer token |

Consoles connect lazily the first time their tab opens. Passwords are masked in the connection bar.

## 7. CLI contract

| Command | Effect |
|---|---|
| `devcli` | Open the TUI |
| `devcli --tab redis` | Open on a given tab |
| `devcli --capture DIR` | Render every tab into a 170×50 simulation screen and write `DIR/NN-tab.html` |
| `devcli --version` | Print the version |

## 8. Local infrastructure

`podman-compose.yml` for development and integration tests:

| Service | Image | Host port | Seed |
|---|---|---|---|
| mysql | `mysql:9` | 3306 | `infra/mysql/init.sql`: `users` with a JSON `profile`, `orders` |
| postgres | `postgres:18` | 5432 | `infra/postgres/init.sql`: same model with `jsonb` |
| cassandra | `cassandra:5.0` (512M heap) | 9042 | `infra/cassandra/init.cql` via `cqlsh` in the container |
| redis | `redis:8` | 6380 | JSON strings, hashes, lists, sorted sets via `redis-cli` in the container |
| loki | `grafana/loki:3.5.5` | 3100 | JSON log lines pushed through `/loki/api/v1/push` |
| grafana | `grafana/grafana:latest` | 3000 | provisioned Loki datasource and one dashboard |

SQLite needs no container: `scripts/setup.sh` builds `.run/devcli.db` from `infra/sqlite/init.sql`.
A small Java program in `infra/jvm/` runs a few named threads (one pair deadlocked) so the Threads tab and its
integration test have a real JVM to dump.

## 9. Testing

Unit tests (`go test ./...`), no network:

| Area | Test intent |
|---|---|
| lexer | a keyword inside a string or comment is not colored as keyword; LogQL `\|=` is one operator |
| completion | prefix beats substring; live table names complete; keyword case follows the user |
| JSON | key order is preserved; nested JSON inside a string cell is expanded in JSON view |
| editor | typing, newline, backspace across lines, Tab accepts popup, Enter runs only when the backend says so |
| SQL split | `;` inside quotes does not split |
| RESP | encoder output bytes; parser for simple, error, integer, bulk, nil and nested arrays over `net.Pipe` |
| top parser | CPU, memory, disk and network byte counters from a captured sample; rates from two samples |
| ps parser | fields, commands with spaces; refusal to signal pid 1, self and parent; identity mismatch refusal |
| podman parser | JSON with names, ports, states; action argument arrays |
| jvm | language detection per marker; dump parsing of states, locks and deadlock section; demangling table §4.9 |
| loki / grafana | `httptest` servers: request paths and parameters, result shaping, error bodies surfaced |
| sqlite backend | real in-memory database: rows, affected counts, words include tables and columns |

Integration tests (`-tags integration`), run by `scripts/test-all.sh` when the compose stack is up: each backend
connects to its container, runs a seeded query, checks rows and completion words; the JVM test dumps the sample
process and expects the deadlock to be detected.

## 10. Scripts

`scripts/` follows the scripts skill: `ports.env`, `common.sh`, `setup.sh` (Go deps, build `bin/devcli`, SQLite
seed, compile the JVM sample), `start-all.sh` (compose up, wait for ports, seed Redis / Cassandra / Loki, start the
JVM sample), `stop-all.sh`, `status.sh`, `test-all.sh` (vet, unit, integration), `ui.sh` (launch the TUI with the
local env), `sql-console.sh` (native console in a container: `mysql`, `postgres`, `cassandra`, `redis`).

## 11. Build order

1. `go.mod`, `internal/syntax` (lexer, JSON, completion) with tests.
2. `internal/backend`: interface, SQL split, SQLite, MySQL, Postgres, Cassandra, RESP client, Loki, Grafana, tests.
3. `internal/sys` and `internal/jvm` collectors and parsers with tests.
4. `internal/ui`: theme, editor, results, console, then dashboard, processes, containers, threads, app shell.
5. `main.go`, capture mode.
6. `infra/` seeds, `podman-compose.yml`, scripts; bring the stack up and run integration tests.
7. Capture printscreens with `npx playwright`, architecture diagram, README.

## 12. Decisions log

| # | Question | Decision |
|---|---|---|
| 1 | TUI library | tview + tcell, matching the existing Harbor TUI and its simulation screen captures |
| 2 | One console or seven | One `Console`, seven `Backend`s |
| 3 | Redis client | Hand-written RESP2 |
| 4 | SQLite driver | mattn/go-sqlite3 (zero Go deps, CGO) |
| 5 | Metrics source | Streaming `top` + `df` + `sysctl`, no gopsutil |
| 6 | Container "ssh" | `exec -it` shell with the TUI suspended |
| 7 | Result formats | Table and JSON toggle; JSON is ordered and colored |
| 8 | Safety | Confirm destructive process and container actions; no SQL guard |
| 9 | Disk and network rates | `netstat -ib` and `ioreg` byte counters instead of `top`'s rounded totals |
| 10 | Loki results | A colored log view instead of a table; the table truncated the log line |
| 11 | Short JSON arrays | Printed on one line when they hold only scalars, so tag lists do not push documents off screen |
| 12 | Grafana readiness | `start-all.sh` waits on `/api/datasources/uid/loki/health`; `/api/health` answers before the Loki plugin registers |
| 13 | Volumes | `df` rows for `/System/Volumes/Data` and `/Volumes/*`; the sealed `/` snapshot always reads about 1% |

## 13. Not in scope

- Linux and Windows metric collectors.
- Per-core CPU graphs.
- Query result paging beyond the first 1000 rows.
- Saved connections, profiles or credential storage.
- Real SSH into containers, container logs and image management.
- Rendering Grafana panels as charts.
