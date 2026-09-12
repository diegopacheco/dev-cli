<p align="center"><img src="assets/logo.png" width="620" alt="devcli"></p>

`devcli` is an all-in-one terminal tool for developers, written in Go. One binary includes:

- a btop-style system dashboard
- a process killer
- a Podman/Docker container manager
- colored JVM thread dumps for Java, Scala, Kotlin and Clojure
- query consoles for MySQL, Postgres, SQLite, Cassandra, Redis, Loki, Grafana and Prometheus

Every console shares one editor: syntax highlighting, line numbers, auto-complete as you type, history, and results shown as tables or colored JSON.

It opens with an ASCII-art splash, finds your running database containers and asks whether to connect to them. `Cmd-K` searches everything and jumps there. Every data source also has a one-shot mode for scripts: `devcli -sql --postgres "select 1"`.

The design is in [design-doc.md](design-doc.md).

## How it Works?

- `main.go` parses the flags. With no mode it opens the TUI. With a mode (`-sql`, `-redis`, `-loki`, …) it runs once, prints the result and exits.
- One tview event loop owns the screen. Background workers collect data and hand it back through `QueueUpdateDraw`, so only the UI goroutine changes what is drawn.
- **Container discovery:** at start, `podman inspect` runs on every running container. Each image name maps to a data store, with its port and credentials read from the container. A prompt then asks which containers each console should use.
- **Cmd-K palette:** a native Go overlay. It fuzzy-matches tabs, commands, discovered containers, containers, JVMs, processes, the live completion words of every console (tables, keys, labels, metrics) and query history. Enter goes to the selected item.
- **Dashboard:** streams `top -l 0 -s 1` for CPU and memory. It reads exact byte counters from `netstat -ib` and `ioreg` for network and disk rates, and draws braille graphs and gradient meters.
- **Consoles:** all eight share one `Console` component and one `Backend` interface.
  - MySQL, Postgres and SQLite use `database/sql`; Cassandra uses `gocql`.
  - Redis uses a hand-written RESP2 client.
  - Loki, Grafana and Prometheus use plain `net/http`.
- **One-shot output:** the same renderers produce tview color tags, which a small converter turns into 24-bit ANSI on a terminal or plain text in a pipe. The ASCII banner goes to stderr, so stdout stays clean for `jq`.

## Architecture

![devcli architecture](assets/architecture.png)

The [SVG source](assets/architecture.svg) uses the Caveat font, a wobble filter, pastel boxes and solid arrows.

## Features

- **ASCII splash:** a gradient `DEVCLI` banner with a spinner while containers are scanned. Any key skips it.
- **Container discovery + connect prompt:** finds running Postgres, MySQL, Cassandra, Redis, Loki, Grafana and Prometheus containers, even with custom ports and passwords. `Space` toggles, `Enter` connects. You don't have to type connection strings for your own containers.
- **Cmd-K search anything:** jump to a tab, run a command, connect a container, open a JVM's thread dump, filter to a process, or insert a table, key, label or metric into its console. Esc clears the search first, a second Esc closes.
- **Cmd-/ shortcuts:** grouped by area, each group with its own icon and color, laid out in columns that fit the screen and scroll inside the modal. Has a search box, a match count, and a message when nothing matches.
- **Cmd-1..9 / Cmd-0, F1..F12:** jump to any tab, even while typing in an editor.
- **One-shot mode:** `-sql --postgres|--mysql|--sqlite`, `-cassandra`, `-redis`, `-loki`, `-grafana`, `-prometheus`, `-ps`, `-containers`, `-threads [PID]`, `-discover`. Also `-json`, `-target`, `-timeout` and `-q`, a colored `--help`, the query from stdin, and exit code 1 on errors.
- **Dashboard (btop style):** CPU history, memory, wired, compressed and swap, volumes, disk and network rates, top processes.
- **Processes:** filter, sort, and SIGTERM/SIGKILL through a dialog where Cancel is selected by default. Before signaling, devcli checks the PID still belongs to the same command.
- **Containers:** running containers first; stop, start, kill, remove, and a shell inside the container with the TUI suspended.
- **Threads:** JVMs tagged Java, Scala, Kotlin or Clojure; state colors, a deadlock banner, and frame names that read like source code.
- **MySQL / Postgres / SQLite / Cassandra:** multi-statement queries, `Enter` runs once the statement ends with `;`. Completion knows live tables and columns.
- **Redis:** any command. JSON values pretty print, hashes render as objects, keys complete.
- **Loki:** LogQL with `labels`, `values`, `:range` and `:limit`, and a colored log view.
- **Grafana:** `search`, `dashboard`, `datasources`, `alerts`, `query DS EXPR`, `get /api/...`.
- **Prometheus:** PromQL instant or `:range` queries, `metrics`, `labels`, `values`, `targets`, `alerts`, `rules`. Metric names complete.
- **Sample data:** `scripts/sample-all.sh start|stop` starts and fills every data store, and runs two JVMs.

## Stack

- **Go 1.26:** one binary; standard library for HTTP, JSON, flags, process execution and signals.
- **tview + tcell:** layout, tables, mouse, paste, the kitty keyboard protocol (which delivers `Cmd` as a modifier), and a simulation screen for tests and screenshots.
- **go-sql-driver/mysql, lib/pq, mattn/go-sqlite3, gocql:** the four database wire protocols.
- **Hand-written code instead of libraries:** the RESP2 client; the Loki, Grafana and Prometheus clients; lexers, fuzzy search, completion, the ordered JSON colorizer, the ANSI converter and container discovery.
- **Podman + podman-compose:** MySQL 9, Postgres 18, Cassandra 5, Redis 8, Loki 3.5, Grafana and Prometheus for local testing.
- **Playwright (npx):** only used to photograph the captured terminal screens for this README.

## Contracts/APIs

devcli has no HTTP server. Its contract is the command line, environment variables and keys.

```bash
devcli --help
```

| Command | Effect |
|---|---|
| `devcli` | Open the TUI: splash, container discovery prompt, dashboard |
| `devcli -sql --postgres QUERY` / `-sql --mysql QUERY` / `--sqlite QUERY` | Run SQL (`--sqllite` also works) |
| `devcli -cassandra QUERY` | Run CQL (`-cql` also works) |
| `devcli -redis COMMAND` | Run Redis commands, one per line |
| `devcli -loki LOGQL` | Query Loki |
| `devcli -grafana COMMAND` | Grafana API commands |
| `devcli -prometheus PROMQL` | Query Prometheus |
| `devcli -ps [FILTER]` / `-containers` / `-threads [PID]` | Processes, containers, JVMs or one thread dump |
| `devcli -discover` | Running data containers, their connect targets and the flag to use |
| `-json` `-target URL` `-timeout 30s` `-no-color` `-q` | JSON output, target override, timeout, plain output, no banner |
| `-tab NAME` `-no-discover` `-capture DIR` `-version` | TUI start tab, skip discovery, HTML screen capture, version |

Every option works with one or two dashes, and options go before the query. When no query is given and stdin is not a terminal, the query is read from stdin. Exit codes: 0 ok, 1 query or connection error, 2 invalid flags.

```bash
devcli -sql --postgres "select kind, count(*) from events group by kind"
devcli -json --sqlite "select * from hosts" | jq '.[].meta.arch'
echo "HGETALL session:9f2c" | devcli -q -redis
devcli -loki '{app="api"} |= "error"'
devcli -prometheus 'sum by (job) (up)'
devcli -threads "$(pgrep -f DevcliJvm)"
```

| Variable | Default |
|---|---|
| `DEVCLI_MYSQL` | `mysql://root:devcli@127.0.0.1:3306/devcli` |
| `DEVCLI_POSTGRES` | `postgres://postgres:devcli@127.0.0.1:5432/devcli?sslmode=disable` |
| `DEVCLI_SQLITE` | `devcli.db` |
| `DEVCLI_CASSANDRA` | `127.0.0.1:9042/devcli` (`host[,host]:port/keyspace`) |
| `DEVCLI_REDIS` | `redis://127.0.0.1:6380/0` (`redis://[user:pass@]host:port/db`) |
| `DEVCLI_LOKI` | `http://127.0.0.1:3100` |
| `DEVCLI_GRAFANA` | `http://admin:devcli@127.0.0.1:3000`; `DEVCLI_GRAFANA_TOKEN` sends a bearer token |
| `DEVCLI_PROMETHEUS` | `http://127.0.0.1:9090` |

| Scope | Keys |
|---|---|
| Global | `Cmd-K` / `Ctrl-K` search, `Cmd-/` / `Ctrl-/` / `?` shortcuts, `Cmd-1..9` `Cmd-0` / `F1..F12` tabs, `Ctrl-N` / `Ctrl-P` next / previous, `Ctrl-Q` quit (also from dialogs) |
| Palette | type to search, `↑↓` move, `Enter` go, `Esc` clear then close |
| Connect prompt | `↑↓` move, `Space` toggle, `a` all, `Enter` / `y` connect, `Esc` / `n` skip |
| Editor | `Ctrl-R` run, `Enter` run or newline, `Tab` complete, `↑↓` popup or history, `Ctrl-L` clear, `Ctrl-T` table/JSON, `Ctrl-E` connection, `F5` reload words, `Esc` cancel query |
| Processes | `/` filter, `o` sort, `x` SIGTERM, `K` SIGKILL, `r` refresh |
| Containers | `Enter`/`e` shell, `s` stop, `S` start, `k` kill, `d` remove, `r` refresh |
| Threads | `Enter` dump, `Tab` focus dump, `/` filter, `s` state filter, `r` refresh |

**Cmd keys in macOS terminals.** tcell turns on the kitty keyboard protocol, so a terminal that forwards `Cmd` sends it to devcli as a modifier. Most terminals keep `Cmd-K` for themselves (it clears the screen), so either use `Ctrl-K` or unbind the key. In Ghostty:

```
keybind = cmd+k=unbind
keybind = cmd+slash=unbind
```

In iTerm2 or Terminal.app, map `Cmd-K` to send the hex code `0x0b`, which is `Ctrl-K`.

Backends talk to these server APIs:

| Backend | Calls |
|---|---|
| Prometheus | `GET /api/v1/status/buildinfo`, `/api/v1/query`, `/api/v1/query_range`, `/api/v1/labels`, `/api/v1/label/<name>/values`, `/api/v1/targets`, `/api/v1/alerts`, `/api/v1/rules` |
| Loki | `GET /loki/api/v1/labels`, `/loki/api/v1/label/<name>/values`, `/loki/api/v1/query_range` |
| Grafana | `GET /api/health`, `/api/search`, `/api/dashboards/uid/<uid>`, `/api/datasources`, `/api/folders`, `/api/v1/provisioning/alert-rules`, `/api/annotations`, `POST /api/ds/query` |
| Redis | RESP2 over TCP, `AUTH` / `SELECT` from the URL, `SCAN` for completion |
| SQL / CQL | `information_schema.columns`, `sqlite_master` + `pragma_table_info`, `system_schema.columns` |
| Discovery | `podman ps -q` + `podman inspect` (or `docker`): `ImageName`, `Config.Env`, `NetworkSettings.Ports`, `State.Running` |

## Key data structures and design decisions

| Structure | Purpose |
|---|---|
| `backend.Backend` | `Connect`, `Execute`, `Words`, `RunsOnEnter`, `Language`: everything a console or one-shot run needs from a data store |
| `backend.Result` | `Columns/Rows` tables, `Logs` log views, `Value` JSON, `Text` pre-rendered text, plus title, message, elapsed |
| `discover.Found` | Kind, container, image, host port and a ready connect URL built from the container's env |
| `ui.PaletteItem` | Kind, title, detail and the `Run` action; the palette only scores and draws them |
| `ui.ShortcutGroup` | Name, icon, color and rows; one table feeds the modal and its filter |
| `cli.Options` | The parsed mode, dialect, query and output options |
| `syntax.Language` / `syntax.Object` | Per-language lexing rules; ordered JSON objects |
| `sys.*`, `jvm.Dump` | Parsed command output and thread dumps, each with a pure parser covered by tests |

- **One renderer, two outputs.** The TUI and one-shot mode use the same table, JSON and log renderers. For one-shot output, `cli.Colorize` turns the tview tags into ANSI escapes or strips them.
- **Discovery asks first.** Containers are only suggested, and nothing connects until you press Enter. Only one container of each kind can be checked, because a console holds one connection.
- **Connection generations.** Picking a container while a console is still connecting to its default bumps a counter. The late result of the first connect is then ignored, and the backend is never used by two goroutines at once.
- **Shortcuts that work while typing.** `Cmd`/`Ctrl` shortcuts are handled before the editor. Plain `q`, `?` and digits only act outside editors, and `Ctrl-Q` quits from anywhere.
- **Few libraries.** Drivers only where a binary protocol needs one; everything else is hand-written.
- **Safe destructive actions.** Kill and remove use a dialog with Cancel selected by default, and a reused PID is refused.

## How to run the app/tests

Requirements: macOS, Go 1.26, Podman with `podman-compose`, a JDK (`java`, `jcmd`), `clojure`, `sqlite3`, `curl`.

```bash
./scripts/setup.sh
./scripts/sample-all.sh start
./scripts/ui.sh
./scripts/test-all.sh
./scripts/sample-all.sh stop
```

`sample-all.sh start` starts the containers and waits until each one answers a real query. It then loads sample data (safe to run again):

| Store | Data |
|---|---|
| Postgres and MySQL | `users` with JSON profiles, `orders`, 200 `events` |
| Cassandra | `devcli.users`, `devcli.events` |
| Redis | JSON strings, a hash, list, sorted set, set and stream |
| SQLite | `.run/devcli.db` with hosts and 100 latency samples |
| Loki | api, worker and web log lines |
| Prometheus | scrapes itself, Loki and Grafana |
| JVMs | a Java program with a deliberate deadlock and a Clojure loop |

`test-all.sh` runs these steps:
1. `bash -n` on every script.
2. `go vet`.
3. Race-enabled unit tests: lexers, fuzzy search, palette navigation, `Cmd-K`/`Cmd-digit` routing, shortcut filtering, the connect prompt, discovery parsing, the ANSI converter, flag parsing, one-shot output, the Redis protocol, all parsers, and Loki, Grafana and Prometheus against fake servers.
4. Integration tests against the running stack, including every one-shot mode.

## Printscreens

These screens are real terminal cells rendered by `devcli -capture` against the local stack, photographed with `npx playwright`. Machine-specific values (processes, containers, CPU) change on every run.

```bash
./bin/devcli -capture .run/capture
for page in .run/capture/*.html; do
  npx playwright screenshot --browser chromium --viewport-size '1560,990' "file://$PWD/$page" "printscreens/$(basename "${page%.html}").png"
done
```

### 1. Splash

The gradient ASCII banner, version and tagline. The spinner line reports the container scan, here 7 data containers found. Any key skips the splash, and it closes by itself after a moment.

![Splash](printscreens/01-splash.png)

### 2. Dashboard

On the top row, the CPU panel draws a heat-colored braille history, with user/sys split, model, cores, uptime, load and process/thread counts. The middle row holds memory meters, volume usage with disk rates, and network graphs. The bottom panel lists top processes by CPU.

![Dashboard](printscreens/02-dashboard.png)

### 3. Processes

All processes sorted by CPU (`▼` marks the sort column). The filter row accepts text or a PID, and the bottom panel shows the selected process. `x` / `K` open the confirm dialog.

![Processes](printscreens/03-processes.png)

### 4. Containers

Running containers first, with state dots, published ports and status. `Enter` opens a shell inside the selected container.

![Containers](printscreens/04-containers.png)

### 5. Threads

The JVM list shows the Java deadlock sample and the Clojure loop, with language badges. The dump shows per-state counts, a red `DEADLOCK` banner naming both threads, lock lines in orange, JDK frames dimmed and application frames bright.

![Threads](printscreens/05-threads.png)

### 6. MySQL

A multi-line join with highlighted keywords, functions and operators. The result table colors numbers orange, JSON documents purple and `NULL` dim.

![MySQL](printscreens/06-mysql.png)

### 7. Postgres

A `jsonb` query in JSON view (`Ctrl-T`): rows become ordered objects, the profile document is expanded, and short arrays stay on one line.

![Postgres](printscreens/07-postgres.png)

### 8. SQLite

A join over `hosts` and `metrics`. The editor holds the next query, and the completion popup offers the live column `meta` and table `metrics` for `met`.

![SQLite](printscreens/08-sqlite.png)

### 9. Cassandra

CQL over `devcli.users`. The `set<text>` column renders as a JSON array.

![Cassandra](printscreens/09-cassandra.png)

### 10. Redis

Three commands in one run: a JSON string pretty printed, `HGETALL` as an object, and a sorted set with scores.

![Redis](printscreens/10-redis.png)

### 11. Loki

Every `env="dev"` stream except cache hits, merged newest first. Levels are colored, labels are purple, and JSON lines are colorized inline.

![Loki](printscreens/11-loki.png)

### 12. Grafana

`dashboard devcli-logs` lists the provisioned panels with type, title, datasource and expression.

![Grafana](printscreens/12-grafana.png)

### 13. Prometheus

A PromQL aggregation per job and the `targets` command showing Prometheus, Loki and Grafana scraped and healthy.

![Prometheus](printscreens/13-prometheus.png)

### 14. Connect prompt

Shown after the splash. Each running data container is listed with its kind, name and connect URL, password masked. The first container of each kind is pre-checked; `Enter` connects the checked ones and `Esc` skips.

![Connect prompt](printscreens/14-connect.png)

### 15. Cmd-K palette

Searching `post` ranks the Postgres tab first, then the connect action for `devcli-postgres`, every Postgres container, a matching process, Prometheus metric names that can be inserted, and actions for the current console. Matched letters are underlined in cyan, and `Enter` goes to the selected item.

![Palette](printscreens/15-palette.png)

### 16. Cmd-/ shortcuts

Every shortcut grouped by area, each group with its own icon and color, flowing in columns with a search box and a match count.

![Shortcuts](printscreens/16-shortcuts.png)

## Scripts

All scripts live in `scripts/` and run from any directory of the repository.

| Script | What it does |
|---|---|
| `./scripts/setup.sh` | Checks tools, downloads Go modules, builds `bin/devcli`, pulls images |
| `./scripts/sample-all.sh start` | Starts every container, waits for each service, loads sample data, starts the sample JVMs |
| `./scripts/sample-all.sh stop` | Stops the sample JVMs and every container |
| `./scripts/start-all.sh` | Checks the binary and runs `sample-all.sh start` |
| `./scripts/status.sh` | Shows every service port as UP or DOWN, plus the sample JVMs |
| `./scripts/test-all.sh` | Runs bash checks, vet, race-enabled unit tests and integration tests |
| `./scripts/ui.sh` | Opens the devcli TUI wired to the local stack |
| `./scripts/stop-all.sh` | Runs `sample-all.sh stop` |
| `./scripts/sql-console.sh` | Opens a native console: `mysql`, `postgres`, `cassandra`, `redis`, `sqlite` or `prometheus` |

Ports are declared in `scripts/ports.env`.

```bash
./scripts/setup.sh
./scripts/start-all.sh
./scripts/status.sh
./scripts/ui.sh
./scripts/stop-all.sh
```
