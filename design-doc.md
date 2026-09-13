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
| 12 | Prometheus | PromQL REPL (v0.2) |

Every console tab (5–12) shares one editor widget: syntax highlighting, line numbers, as-you-type
auto-complete (static keywords plus live names pulled from the connected server), history, and a results pane
that renders tables or pretty, colored JSON.

### 1.1 v0.2 additions

| Feature | Summary | Section |
|---|---|---|
| Cmd-K palette | Native Go overlay that searches tabs, commands, containers, JVMs, processes, completion words and history; Enter goes there | §5.4 |
| Cmd-/ shortcuts | Grouped, colored, searchable modal that always fits the screen | §5.5 |
| ASCII splash | Gradient `DEVCLI` banner at start, also printed by `--help` and one-shot runs | §5.6 |
| Container discovery | Finds running Cassandra, Redis, Loki, Grafana, Prometheus, Postgres and MySQL containers and asks which ones to connect | §4.10 |
| One-shot mode | `-sql --postgres`, `--sqlite`, `-redis`, `-loki`, `-grafana`, `-prometheus`, `-cassandra`, `-ps`, `-containers`, `-threads`, `-discover`, `--help` | §7 |
| Prometheus | Eighth console and one-shot mode | §4.2 |
| Sample data | `scripts/sample-all.sh start` / `stop` starts and fills every data store | §8, §10 |

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
9. **"Cmd + K" in a terminal** only reaches a program when the terminal forwards Super. tcell turns on the kitty
   keyboard protocol, so Ghostty, kitty and WezTerm report `Cmd` as `ModMeta` once the terminal's own `Cmd-K`
   binding is removed. `Ctrl-K` always works as the same shortcut. The other macOS app shortcuts (zoom, print
   screen, full screen, window position) belong to the terminal emulator, not to a TUI, and are not implemented.
10. **"Ask the user if they want to connect"** means a prompt after the splash that lists every running data
    container with its connect URL. Nothing connects until the user confirms.

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

### 4.0 Two entry points

`main.go` calls `cli.Parse`. No mode flag opens the TUI (`ui.App.Run`). A mode flag runs `cli.Execute` once, prints
the results through the same renderers the TUI uses, and exits. Exit code 0 means ok, 1 a query or connection
error, 2 invalid flags.

### 4.1 Package layout

```
main.go                     parse flags, detect terminals, hand off to cli.Main
internal/cli/cli.go         flag parsing, one-shot execution, TUI launch, banner
internal/cli/ansi.go        tview color tags → 24-bit ANSI or plain text
internal/cli/help.go        colored --help
internal/discover/discover.go  podman/docker inspect → kind, host port, connect URL
internal/ui/palette.go      Cmd-K overlay
internal/ui/fuzzy.go        token fuzzy scoring with word-start bonuses
internal/ui/shortcuts.go    Cmd-/ overlay and the shortcut table
internal/ui/splash.go       ASCII banner and splash overlay
internal/ui/prompt.go       discovered-container connect prompt
internal/backend/prometheus.go
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
    Text    string
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
| Loki | `all`, `labels`, `values <label>`, `:range 30m`, `:limit 100`, otherwise LogQL via `query_range` | commands, label names and values | always |
| Prometheus | `all`, `:range 30m` / `:range off`, `:step 15s`, `metrics [text]`, `labels`, `values <label>`, `targets`, `alerts`, `rules`, otherwise PromQL via `query` or `query_range` (step = range/120) | commands, metric names, label names | always |
| Grafana | `all`, `health`, `search [q]`, `dashboard <uid>`, `datasources`, `folders`, `alerts`, `annotations`, `query <ds-uid> <expr>`, `get <path>` | commands, dashboard uids, datasource uids | always |

#### The `all` catalog

A new user types a function name like `avg_over_time` and gets either 0 series (Prometheus reads it as a metric
name) or `parse error … unexpected IDENTIFIER` (Loki). `all` answers "what can I type here" from the live server, as
several results, each rendered as its own table:

| Console | Sections |
|---|---|
| Loki | commands · ready queries · labels with up to 10 values · streams (`/loki/api/v1/series` with one `match[]` per label) · pipeline stages · functions |
| Grafana | commands · ready commands · health · datasources · folders · dashboards · alert rules |
| Prometheus | commands · ready queries · metrics with type and help (`/api/v1/metadata`) · labels with up to 8 values · targets · functions |

- **Ready queries come from the server, not a fixed list.**
  - Loki picks a real `app`, `service_name`, `job` or `container` value, and adds a per-level count when `level` exists.
  - Grafana writes `query <uid> …` for each Loki and Prometheus datasource, plus `dashboard <uid>` for each dashboard.
  - Prometheus picks a counter, a gauge and a histogram, preferring well-known names over alphabetical noise.
- **Wide tables.** Command, ready-query and function tables set `Result.Wide`, so cells are not cut at 48 characters.
- **Loading ready queries.** The TUI takes over the mouse, which makes copying awkward. Each console keeps the rows of its last `ready` section, and the Cmd-K palette lists them (kind `ready`, weight 45). `Enter` loads one into the editor.
- **Partial access.** A section the credentials cannot read shows `unavailable: …` instead of failing the whole listing.
- **Bare function names.** A function name alone returns its usage row without querying the server.
- **Loki parse errors** keep the server message and add a pointer to a stream selector and `all`.
- **0 series.** A Prometheus query with no series says so and points to `all`.
- **Discoverability.**
  - After connecting, the three consoles say "type all and press Enter".
  - The palette has a "List everything available (all)" action for them.
  - `all` completes and highlights as a keyword.

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

### 4.10 Container discovery

`podman ps -q` then one `podman inspect` over the ids (`docker` when podman is missing). For each running container:

| Step | Rule |
|---|---|
| Kind | last image path segment without tag or digest, equal to or prefixed by a token: `postgres postgis timescaledb pgvector`, `mysql mariadb percona`, `cassandra scylla`, `redis valkey keydb`, `loki`, `grafana`, `prometheus`; names containing `exporter` are skipped |
| Port | `NetworkSettings.Ports["<default port>/tcp"][0].HostPort`; a container without a published port is not reachable and is skipped |
| Host | `HostIp`, with empty, `0.0.0.0` and `::` mapped to `127.0.0.1` |
| Credentials | read from the container env: the official Postgres, MySQL/MariaDB, Redis and Grafana admin variables (Grafana falls back to `admin/admin`) |

`grafana/loki` is Loki because only the last path segment counts. At TUI start the scan runs while the splash is
up. When it finds containers, the connect prompt opens after the splash:
- The first container of each kind is pre-checked.
- Checking another container of the same kind unchecks the first, because a console holds one connection.
- `Enter` retargets the chosen consoles.

The Console guards reconnects with a generation counter and a backend mutex. If a lazy connect to the default
target is still running, its late result is dropped.

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
| Global | `Cmd-K` / `Ctrl-K` palette, `Cmd-/` / `Ctrl-/` / `?` shortcuts, `Cmd-1..9`, `Cmd-0`, `F1`–`F12` jump, `Ctrl-N` / `Ctrl-P` next / previous tab, click a tab, `Ctrl-Q` quit from anywhere |
| Lists (tabs 1–4) | `↑↓` / `j k` select, `/` filter, `Esc` clear filter, `q` quit, `r` refresh |
| Editor | `Ctrl-R` run buffer, `Enter` newline or run (§4.2), `Tab` accept completion, `↑↓` popup or lines, history on first / last line, `Ctrl-L` clear buffer (Ctrl-K opens the palette since v0.2), `Ctrl-T` table / JSON, `PgUp/PgDn` scroll results, `Ctrl-E` edit connection, `F5` reload completions, `Esc` close popup or cancel running query |

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

### 5.4 Cmd-K palette

A custom tview primitive drawn as a centered overlay page, with no library behind it.
- **Opening:** `Cmd-K`, reported as `KeyRune 'k'` with `ModMeta`, or `Ctrl-K`. The app input capture checks for it
  before any tab, so the palette opens from inside an editor too.
- **Empty query:** shows tabs and commands.
- **Typed query:** adds these sources.

| Kind | Source | Enter |
|---|---|---|
| tab | every tab | switch |
| action | shortcuts, discover, refresh lists, quit; for the current console run, table/JSON, reload words, clear, reconnect | run it |
| connect | discovered containers | retarget that console and switch to it |
| ready | ready queries from the last `all` of each console; the current console's show even with an empty query | switch and load the query into the editor |
| container | Containers list | switch and select the row |
| jvm | Threads list | switch, select, dump |
| process | Processes list, or the dashboard sample before that tab opened | switch and filter to the pid |
| mysql, postgres, redis, … | each console's live completion words | switch and insert the word |
| history | each console's history | switch and load the query |

Scoring (`ui.Fuzzy`) splits the query into tokens, and every token must match the title or the kind and detail.
- **Substring match:** scores higher, with bonuses for a match at the start or at a word start.
- **Subsequence match:** scores per letter, with bonuses for word starts and consecutive letters.
- **Title matches** count double, and matched title letters are underlined.
- **Kind weight:** tab 60, connect 50, ready 45, action 40, container and jvm 30, words 15, history 10, process 0. Navigation
  targets rank above completion noise.
- **Limit:** the list keeps the top 200.

Keys: `↑↓`, `PgUp/PgDn`, `Enter`, `Esc` (clears the query first, then closes), `Ctrl-K` (closes).

### 5.5 Cmd-/ shortcuts

One `shortcutGroups` table feeds both the modal and its filter. Each group has its own icon glyph and color; the icon,
title, rule and keys of a group share that color. Groups flow into 3 columns at 110+ columns wide, 2 at 72+, and
otherwise 1, each group placed in the shortest column. The modal takes at most 92% of the screen height and
scrolls inside itself, so no row is cut off. The search box is focused and filters as you type:
- matching a group title keeps the whole group;
- otherwise only matching rows stay;
- a count shows how many shortcuts matched, with a plain message when none do.

`Esc` clears the search first and closes on an empty box.

### 5.6 ASCII splash

The banner is built from six letters in the ANSI Shadow style, each row joined per letter so widths always line up
(a test enforces equal row widths). In the TUI the splash is an overlay:
- **Banner:** block cells follow a moving cyan → magenta → cyan gradient, and shadow cells are dim.
- **Status:** a spinner line reports the container scan.
- **Closing:** after 1.8 s or on any key, and then the connect prompt opens if the scan found containers.

`--help` prints the banner to stdout; one-shot runs print it to stderr only when stderr is a terminal and `-q` is
not set, so pipes stay clean.

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
| `DEVCLI_PROMETHEUS` | `http://127.0.0.1:9090` |

Consoles connect lazily the first time their tab opens, or to a discovered container once the user picks it.
Passwords are masked in the connection bar, the prompt, the palette and `-discover` output.

## 7. CLI contract

| Command | Effect |
|---|---|
| `devcli` | Open the TUI: splash, connect prompt, dashboard |
| `devcli -sql --postgres QUERY`, `-sql --mysql QUERY`, `--sqlite QUERY` (`--sqllite`) | One-shot SQL; a dialect flag alone implies `-sql` |
| `devcli -cassandra QUERY` (`-cql`), `-redis COMMAND`, `-loki LOGQL`, `-grafana COMMAND`, `-prometheus PROMQL` | One-shot query |
| `devcli -ps [FILTER]`, `-containers`, `-threads [PID]`, `-discover` | One-shot listings and thread dump |
| `-json`, `-target URL`, `-timeout 30s`, `-no-color`, `-q` | Output and connection options |
| `--help`, `--version`, `-tab NAME`, `-no-discover`, `-capture DIR` | Help with banner, version, TUI start tab, skip scan, 170×50 HTML capture of 16 screens |

Rules:
- Go's `flag` package accepts one or two dashes, and options must come before the query.
- The query is the remaining arguments joined with spaces; when there are none and stdin is not a terminal, stdin is read.
- More than one mode or dialect is an error, never a guess.

| Output | Terminal | Pipe |
|---|---|---|
| Banner | stderr, unless `-q` | none |
| Result header (`▶ title · rows · time`) | yes | no |
| Colors | 24-bit ANSI, unless `-no-color` / `NO_COLOR` | none |
| `-json` | colored JSON per result | plain JSON that `jq` parses |

## 8. Local infrastructure

`podman-compose.yml` for development and integration tests:

| Service | Image | Host port | Seed |
|---|---|---|---|
| mysql | `mysql:9` | 3306 | `infra/mysql/init.sql`: `users` with a JSON `profile`, `orders` |
| postgres | `postgres:18` | 5432 | `infra/postgres/init.sql`: same model with `jsonb` |
| cassandra | `cassandra:5.0` (512M heap) | 9042 | `infra/cassandra/init.cql` via `cqlsh` in the container |
| redis | `redis:8` | 6380 | JSON strings, hashes, lists, sorted sets via `redis-cli` in the container |
| loki | `grafana/loki:3.5.5` | 3100 | JSON log lines pushed through `/loki/api/v1/push` |
| grafana | `grafana/grafana:latest` | 3000 | provisioned Loki and Prometheus datasources and one dashboard |
| prometheus | `prometheus/prometheus:latest` | 9090 | scrapes itself, Loki and Grafana every 5 s |

SQLite needs no container: `scripts/sample-all.sh start` builds `.run/devcli.db` from `infra/sqlite/init.sql`.

`scripts/sample-all.sh start` waits until every service answers a real query, not just an open port. It then loads
every seed on each run. The seeds drop and recreate their tables, so running start twice gives the same data:
- **Postgres and MySQL:** 200 generated `events` rows.
- **Redis:** includes a stream.
- **SQLite:** 100 latency samples.

`stop` stops the sample JVMs and the containers and waits for every port to close.

### 8.1 Java 25 thread dump sample

`sample/java25/src` is a multi-file source program (JEP 458), launched with `java sample/java25/src/DevcliJvm.java`,
so there is no build tool. `DevcliJvm.java` is a compact source file with `void main()`. The other files use
`import module`, `ScopedValue`, records, a sealed `Order` hierarchy and pattern-matching `switch`, all final in
Java 25.

| File | Threads | Dump shows |
|---|---|---|
| `Orders.java` | `order-producer`, `order-worker-1..4` | queue producer and consumers; `ScopedValue.where(...).run` frames; SHA-256 CPU work |
| `Deadlocks.java` | `ledger-writer` + `inventory-writer`, `payment-capture` + `payment-refund` | a monitor deadlock and a `ReentrantLock` deadlock |
| `Contention.java` | `report-exporter`, `report-reader-1..3` | BLOCKED readers behind a holder, not a deadlock |
| `Background.java` | `cpu-hasher`, `cache-refresher`, `event-listener`, virtual thread carriers | RUNNABLE CPU work, scheduled TIMED_WAITING, `Object.wait` |
| `HttpTraffic.java` | `HTTP-Dispatcher`, `stats-poller`, `HttpClient-*` | JDK `HttpServer` on an ephemeral loopback port and an `HttpClient` poller |

`scripts/sample-java.sh start` checks that `java` is 25 or newer. It then waits for `jcmd` to attach and for the
app's own "sample running" line: `jcmd` answers while the launcher is still compiling, before any thread exists.

The sample exposed a parser bug. jcmd prints one "Found one Java-level deadlock" report per deadlock, and the parser
stopped after the first, so the `ReentrantLock` pair was never marked. It also missed the plural total
"Found 2 deadlocks.". Both are fixed and covered by a test built from the real output.

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
| prometheus | instant vs `:range` path and step, targets health, metric and label completion |
| discover | connect URLs from env and ports, exporters skipped, unpublished ports skipped, `grafana/loki` is Loki, tab order |
| palette | Cmd-K and Ctrl-K open it from an editor; tab name ranks first; pid finds its process; live key inserts into Redis; Esc clears then closes; tabs and connect outrank noise |
| shortcuts | title match keeps a group, row match keeps rows, zero count, narrow screen scrolls |
| connect prompt | one checked per kind, Enter connects checked; consoles retarget |
| keys | Cmd-digit switches tabs while typing |
| cli | mode parsing and ambiguity errors, ANSI conversion and unescaping, piped `-json` parses, terminal banner and colors, `-q`, exit codes, help lists every mode, `-target` override |

Integration tests (`-tags integration`), run by `scripts/test-all.sh` when the compose stack is up:
- Each backend connects to its container, runs a seeded query, and checks rows and completion words.
- The JVM test dumps the sample process and expects the deadlock to be detected.
- The CLI test runs every one-shot mode against the stack.

Unit tests run with `-race`, which found and fixed a data race on the tabs' visibility flags. A pty run sends the real
kitty protocol bytes for `Cmd-K` (`ESC [107;9u`) and `Cmd-/` (`ESC [47;9u`) and confirms the palette and the
shortcuts open and that `Ctrl-Q` exits.

## 10. Scripts

`scripts/` follows the scripts skill: `ports.env`, `common.sh`, `setup.sh` (Go deps, build `bin/devcli`, pull
images), `sample-all.sh start|stop` (compose up, wait for services, load every seed and start the JVM samples; or
stop everything), `start-all.sh` / `stop-all.sh` (delegate to `sample-all.sh`), `status.sh`, `test-all.sh` (vet, unit, integration), `ui.sh` (launch the TUI with the
local env), `sql-console.sh` (native console in a container: `mysql`, `postgres`, `cassandra`, `redis`, `sqlite`,
`prometheus`), `sample-java.sh start|stop|status|dump` (Java 25 sample, §8.1).

`install-macos.sh` and `uninstall-macos.sh` put exactly one `devcli` on the user's PATH:

| Step | Rule |
|---|---|
| Location | `${DEVCLI_INSTALL_DIR:-$HOME/.local/bin}/devcli` |
| One version | install always runs uninstall first, then rebuilds from source |
| Copy | to `devcli.tmp`, `chmod 0755`, then `mv`; an in-place overwrite of a signed arm64 binary can get it killed by macOS |
| Check | the installed binary must answer `-version` |
| PATH | when the directory is not on `PATH`, append one `export PATH=...` line to `~/.zshrc` (`~/.bash_profile` for bash), never twice |
| Shadowing | warn when another `devcli` elsewhere on `PATH` is not managed by the script; never delete it |
| Uninstall | stop running `devcli`, remove the binary and any `.tmp`; running it twice is fine |

Both scripts were verified against a throwaway `HOME` and install directory: a first install adds the PATH line, a
second install keeps one line and one binary, an install into a directory already on PATH leaves the rc file
alone, and a second uninstall still exits 0.

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
| 14 | Cmd-K | Native overlay primitive; `ModMeta` via the kitty protocol plus `Ctrl-K`; editor clear moved to `Ctrl-L` |
| 15 | Palette ranking | Fuzzy score plus a per-kind weight so tabs and connect actions beat long metric names |
| 16 | Discovery | `inspect` JSON, last image segment, env credentials; a prompt instead of auto-connecting |
| 17 | One-shot rendering | Reuse the TUI renderers and convert tview tags to ANSI instead of writing a second renderer |
| 18 | Banner stream | stderr for one-shot runs so stdout is pipe-safe; stdout for `--help` |
| 19 | Seeds | Applied by `sample-all.sh` through `podman exec` on every start instead of `docker-entrypoint-initdb.d`, so data reloads without recreating containers |
| 20 | Ctrl-Q | Handled before overlays so quitting always works |
| 21 | Discoverability of Loki, Grafana, Prometheus | An `all` catalog from the live server, function usage for bare function names, hints on parse errors and empty results |
| 22 | Ready queries | Built from real labels, metrics and datasources; wide tables so they are never truncated |
| 23 | JVM sample | One Java 25 multi-file source app in `sample/java25` replaces the single-file `infra/jvm` program |
| 24 | Install location | `~/.local/bin`, uninstall before install, atomic rename, PATH line only when missing |
| 25 | "Frozen" after clicking Threads | Root cause: tview `Pages` hands a mouse event to the top page, then to the pages below if it is not consumed. The connect prompt used `Box`'s default handler, which consumes only mouse-down, so the click reached the tab bar. The tab switched and focus moved under a still-visible prompt that no longer got Esc, `n` or `q`. Fix: every overlay is added through `dialogLayer`, which always consumes mouse events; `App.keys` refocuses the top dialog when focus wandered; the prompt handles its own clicks; the confirm dialog goes through the same layer. Regression tests click a tab behind the prompt and behind a confirm dialog |
| 26 | Late discovery results | If the container scan finishes after the user pressed a key or clicked a tab, show a status-line hint and keep the containers in Cmd-K instead of opening the prompt over their work; `Discover containers` from the palette still opens it |

## 13. Not in scope

- Linux and Windows metric collectors.
- Per-core CPU graphs.
- Query result paging beyond the first 1000 rows.
- Saved connections, profiles or credential storage.
- Real SSH into containers, container logs and image management.
- Rendering Grafana panels as charts.
