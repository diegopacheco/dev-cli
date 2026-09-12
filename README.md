<p align="center"><img src="assets/logo.png" width="620" alt="devcli"></p>

`devcli` is an all-in-one terminal UI for developers, written in Go. One binary gives you a btop-style system dashboard, a process killer, a Podman/Docker container manager, colored JVM thread dumps for Java, Scala, Kotlin and Clojure, and query consoles for MySQL, Postgres, SQLite, Cassandra, Redis, Loki and Grafana. Every console has the same editor: syntax highlighting, line numbers, as-you-type auto-complete, history, and results as tables or pretty, colored JSON.

The design is in [design-doc.md](design-doc.md).

## How it Works?

- One tview event loop owns the screen. Background workers collect data and hand results back through `QueueUpdateDraw`, so only the UI goroutine changes what is drawn.
- The dashboard streams `top -l 0 -s 1` for CPU and memory. Each second it also reads exact byte counters from `netstat -ib` and `ioreg` for network and disk rates, and draws them as braille graphs with gradient meters.
- Processes come from `ps`, containers from `podman ps --format json` (with a `docker` fallback), and JVMs from `ps` plus `jcmd <pid> Thread.print -l`.
- The seven consoles share one `Console` component and one `Backend` interface. MySQL, Postgres and SQLite go through `database/sql`, and Cassandra through `gocql`. Redis uses a small hand-written RESP2 client, and Loki and Grafana use plain `net/http`.
- The editor is a custom tview primitive. It runs a per-language lexer for colors and draws a gutter with line numbers. Completion words are the language keywords plus names read from the live server: tables and columns, Redis keys, Loki labels and values, Grafana dashboard and datasource uids.
- Results render as box-drawn tables or ordered, colored JSON. Loki results render as a colored log view. A text cell that holds a JSON document is expanded as JSON.

## Architecture

![devcli architecture](assets/architecture.png)

The [SVG source](assets/architecture.svg) uses the Caveat font, a wobble filter, pastel boxes and solid arrows.

## Features

- **Dashboard (btop style):** CPU history graph, load, cores, uptime; memory, wired, compressed, swap meters; volume usage; disk and network rates; top processes. Gives a one-look health check of the laptop.
- **Processes:** Filter by text, sort by CPU, memory or PID, and send SIGTERM or SIGKILL through a red confirm dialog with Cancel focused. Before signaling, devcli checks that the PID still belongs to the same command, and it refuses init, itself and its parent.
- **Containers:** Running containers are listed first. Keys stop, start, kill and remove a container, and `Enter` opens a shell inside it: the TUI suspends, runs `podman exec -it`, and comes back when the shell exits.
- **Threads:** Lists local JVMs and tags each as Java, Scala, Kotlin or Clojure. Thread dumps are colored by state, deadlocks show in a red banner, and frames read like source code (`my-app.core/handle-request`, `app.Main.run (lambda)`, `app.WorkerKt.main (coroutine)`).
- **MySQL / Postgres / SQLite:** Multi-statement SQL, `Enter` runs once the statement ends with `;`, and affected-row counts are shown. Completion knows your tables and columns.
- **Cassandra:** CQL with `USE` support and completion from `system_schema`. Collections and JSON text render as JSON.
- **Redis:** Any command, with quoting. JSON string values pretty print, `HGETALL` renders as an object, and keys complete from `SCAN`.
- **Loki:** LogQL REPL with `labels`, `values <label>`, `:range 30m` and `:limit 100`. Streams are merged newest first into a log view with a colored level for each line.
- **Grafana:** `health`, `search`, `dashboard <uid>`, `datasources`, `folders`, `alerts`, `annotations`, `query <ds-uid> <expr>` and `get /api/...`. Lets you run a datasource query without leaving the terminal.
- **Table / JSON toggle:** `Ctrl-T` flips every result between a table and ordered, colored JSON.
- **Connection bar:** Passwords are masked, `Ctrl-E` edits the target, and consoles connect the first time their tab opens.

## Stack

- **Go 1.26:** one binary, standard library for HTTP, JSON, process execution and signals.
- **tview + tcell:** layout, tables, modals, mouse, paste, and a simulation screen used by tests and screenshot capture.
- **go-sql-driver/mysql:** MySQL wire protocol with modern auth plugins.
- **lib/pq:** Postgres driver with no transitive dependencies and SCRAM auth.
- **mattn/go-sqlite3:** SQLite with no Go dependencies (CGO).
- **gocql:** Cassandra native protocol.
- **Hand-written code instead of libraries:** RESP2 client, Loki and Grafana clients, the syntax lexer, completion, and the ordered JSON colorizer.
- **Podman + podman-compose:** local MySQL 9, Postgres 18, Cassandra 5, Redis 8, Loki 3.5 and Grafana for development and integration tests.
- **Playwright (npx):** only used to photograph the captured terminal screens for this README.

## Contracts/APIs

devcli has no HTTP server. Its contract is the command line, environment variables and keys.

| Command | Effect |
|---|---|
| `devcli` | Open the TUI on the dashboard |
| `devcli --tab redis` | Open on a tab: `dashboard processes containers threads mysql postgres sqlite cassandra redis loki grafana` |
| `devcli --capture DIR` | Render every tab against live data into `DIR/NN-tab.html` and exit |
| `devcli --version` | Print the version |

| Variable | Default |
|---|---|
| `DEVCLI_MYSQL` | `mysql://root:devcli@127.0.0.1:3306/devcli` |
| `DEVCLI_POSTGRES` | `postgres://postgres:devcli@127.0.0.1:5432/devcli?sslmode=disable` |
| `DEVCLI_SQLITE` | `devcli.db` |
| `DEVCLI_CASSANDRA` | `127.0.0.1:9042/devcli` (`host[,host]:port/keyspace`) |
| `DEVCLI_REDIS` | `redis://127.0.0.1:6380/0` (`redis://[user:pass@]host:port/db`) |
| `DEVCLI_LOKI` | `http://127.0.0.1:3100` |
| `DEVCLI_GRAFANA` | `http://admin:devcli@127.0.0.1:3000` (basic auth from the URL) |
| `DEVCLI_GRAFANA_TOKEN` | empty; when set it is sent as `Authorization: Bearer` |

| Scope | Keys |
|---|---|
| Global | `Ctrl-N` / `Ctrl-P` next / previous tab, `F1`–`F11` jump, click a tab, `1`–`9` outside editors, `q` outside editors or `Ctrl-Q` quit |
| Editor | `Ctrl-R` run, `Enter` run or newline, `Tab` accept completion, `↑↓` completion or history, `Ctrl-K` clear, `Ctrl-T` table/JSON, `Ctrl-E` connection, `F5` reload completion words, `PgUp/PgDn` scroll results, `Esc` cancel a running query |
| Processes | `/` filter, `o` sort, `x` SIGTERM, `K` SIGKILL, `r` refresh, `Esc` clear filter |
| Containers | `Enter`/`e` shell, `s` stop, `S` start, `k` kill, `d` remove, `r` refresh |
| Threads | `Enter` dump, `Tab` focus the dump, `/` filter threads, `s` cycle state filter, `r` refresh |
| Confirm dialog | `y` confirm, `n` / `Esc` / `Enter` on the focused Cancel button cancel |

Backends talk to these server APIs:

| Backend | Calls |
|---|---|
| Loki | `GET /loki/api/v1/labels`, `GET /loki/api/v1/label/<name>/values`, `GET /loki/api/v1/query_range` |
| Grafana | `GET /api/health`, `/api/search`, `/api/dashboards/uid/<uid>`, `/api/datasources`, `/api/folders`, `/api/v1/provisioning/alert-rules`, `/api/annotations`, `POST /api/ds/query` |
| Redis | RESP2 arrays over TCP, `AUTH` / `SELECT` from the URL, `SCAN` for completion |
| SQL | `information_schema.columns` (MySQL, Postgres), `sqlite_master` + `pragma_table_info` (SQLite) |
| Cassandra | `system_schema.columns` for completion |

## Key data structures and design decisions

| Structure | Purpose |
|---|---|
| `backend.Backend` | `Connect`, `Execute`, `Words`, `RunsOnEnter`, `Language`: the only thing a console needs to know about a data store |
| `backend.Result` | `Columns/Rows` for tables, `Logs` for log views, `Value` for JSON, plus title, message and elapsed time |
| `syntax.Language` | Keywords, functions, comments, operators, extra word runes and whether bind variables exist |
| `syntax.Object` | Ordered JSON object (`[]Pair`), so output keeps the server's key order |
| `ui.Editor` | Lines as `[][]rune`, cursor, scroll, completion popup and history, drawn cell by cell |
| `sys.Metrics` / `sys.Volume` / `sys.Process` / `sys.Container` | Parsed command output, each with a pure parser covered by tests |
| `jvm.Dump` / `jvm.Thread` | Threads with state, frames, lock lines and a deadlock flag |

- **One console, seven backends.** The editor, completion, history, results and status line exist once.
- **Few libraries.** Drivers only where a binary protocol needs one; Redis, Loki, Grafana, highlighting and JSON are hand-written.
- **Exact counters for rates.** `top` rounds network and disk totals to gigabytes, so rates come from `netstat -ib` and `ioreg` bytes.
- **Enter runs only complete statements.** SQL and CQL need a trailing `;`; Redis, Loki and Grafana run on Enter; `Ctrl-R` always runs.
- **Safe destructive actions.** Kill and remove open a dialog with Cancel focused; a reused PID is refused.
- **Commands without a shell.** Every external command is an argument array with a timeout and a bounded output buffer.

## How to run the app/tests

Requirements: macOS, Go 1.26, Podman with `podman-compose`, a JDK (`java`, `jcmd`), `clojure`, `sqlite3`, `curl`.

```bash
./scripts/setup.sh
./scripts/start-all.sh
./scripts/ui.sh
./scripts/test-all.sh
./scripts/stop-all.sh
```

`start-all.sh` starts the containers, waits until every service answers a real query, seeds Redis, Cassandra and Loki, and launches a Java program with a deliberate deadlock plus a Clojure loop so the Threads tab has JVMs to dump. `test-all.sh` runs `bash -n`, `go vet`, the unit tests (lexer, completion, JSON, RESP, SQL split, parsers for top/netstat/ioreg/df/ps/podman, thread dumps and demangling, Loki and Grafana against `httptest`, SQLite in memory, editor keys, rendering and key routing), then the integration tests tagged `integration` against the running stack.

```bash
go build -o bin/devcli .
DEVCLI_REDIS=redis://my-host:6379/0 ./bin/devcli --tab redis
```

## Printscreens

The screens below are real terminal cells rendered by `devcli --capture` against the local stack and photographed with `npx playwright`. Machine-specific values (processes, containers, CPU) change on every run.

```bash
./bin/devcli --capture .run/capture
for page in .run/capture/*.html; do
  npx playwright screenshot --browser chromium --viewport-size '1560,990' "file://$PWD/$page" "printscreens/$(basename "${page%.html}").png"
done
```

### 1. Dashboard

On the top row, the CPU panel draws a heat-colored braille history, with user/sys split, model, cores, uptime, load average and process/thread counts. The middle row holds memory meters, volume usage with disk read/write rates, and network in/out graphs with totals. The bottom panel lists top processes by CPU, each with a gradient bar.

![Dashboard](printscreens/01-dashboard.png)

### 2. Processes

All processes, sorted by CPU (the `▼` marks the sort column). The filter row accepts free text or a PID, and the bottom panel shows the selected process. `x` and `K` open the confirm dialog.

![Processes](printscreens/02-processes.png)

### 3. Containers

Podman containers with running ones first, green dots for running and dim ones for exited, published ports in orange, and the full status. `Enter` shells into the selected container.

![Containers](printscreens/03-containers.png)

### 4. Threads

The JVM list on the left shows the Java deadlock sample and the Clojure loop, with language badges. The dump on the right starts with per-state counts and a red `DEADLOCK` banner that names the two threads waiting on each other. Below that, each thread shows its state, CPU time, lock lines in orange, JDK frames dimmed and application frames bright.

![Threads](printscreens/04-threads.png)

### 5. MySQL

A multi-line join with highlighted keywords, functions, operators and line numbers. The result table colors numbers orange, JSON documents purple and `NULL` dim.

![MySQL](printscreens/05-mysql.png)

### 6. Postgres

A `jsonb` query switched to JSON view with `Ctrl-T`. Rows become ordered objects, the `profile` document is expanded, and short tag arrays stay on one line.

![Postgres](printscreens/06-postgres.png)

### 7. SQLite

A join over the seeded `hosts` and `metrics` tables. The editor now holds the next query, and the completion popup offers the live column `meta` and table `metrics` for `met`.

![SQLite](printscreens/07-sqlite.png)

### 8. Cassandra

A CQL select over `devcli.users`. The `set<text>` column renders as a JSON array and the `profile` text column as JSON.

![Cassandra](printscreens/08-cassandra.png)

### 9. Redis

Three commands in one run. The JSON string in `user:1` is pretty printed, `HGETALL` becomes an object, and `ZRANGE ... WITHSCORES` is a list. A key like `user:1` is highlighted as one word.

![Redis](printscreens/09-redis.png)

### 10. Loki

A LogQL query over every `env="dev"` stream, merged newest first. Levels are colored (`ERROR` red, `WARN` orange, `INFO` green), labels are purple, JSON log lines are colorized inline, and plain text lines stay plain.

![Loki](printscreens/10-loki.png)

### 11. Grafana

`dashboard devcli-logs` lists the provisioned dashboard's panels with type, title, datasource uid and LogQL expression. `Ctrl-T` shows the full dashboard JSON.

![Grafana](printscreens/11-grafana.png)

## Scripts

All scripts live in `scripts/` and run from any directory of the repository.

| Script | What it does |
|---|---|
| `./scripts/setup.sh` | Checks tools, downloads Go modules, builds `bin/devcli`, seeds `.run/devcli.db`, pulls images |
| `./scripts/start-all.sh` | Starts every container, waits for each service, seeds data, starts the sample JVMs |
| `./scripts/status.sh` | Shows every service port as UP or DOWN, plus the sample JVMs |
| `./scripts/test-all.sh` | Runs bash syntax checks, vet, unit tests and integration tests |
| `./scripts/ui.sh` | Opens the devcli TUI wired to the local stack |
| `./scripts/stop-all.sh` | Stops the sample JVMs and every container |
| `./scripts/sql-console.sh` | Opens a native console: `mysql`, `postgres`, `cassandra`, `redis` or `sqlite` |

Ports are declared in `scripts/ports.env`.

```bash
./scripts/setup.sh
./scripts/start-all.sh
./scripts/status.sh
./scripts/ui.sh
./scripts/stop-all.sh
```
