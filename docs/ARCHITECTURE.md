# Architecture of the daemon

How the Go packages in `daemon/` depend on each other and which goroutines share which state, as read from the code on 2026-11-12.

## Packages

Imports come from `go list -f '{{.ImportPath}} {{.Imports}}' ./...` (internal packages only, prefix dropped).

| Package | Purpose | Imports (internal) |
|---|---|---|
| `cmd/benchd` | Daemon entry: wires every manager, starts the API, orders shutdown | api, binman, caddy, dnsstub, doctor, elevate, mail, newapp, paths, phppool, registry, server, services, setup, sitedelete, sites, supervisor, tasks, token, version |
| `cmd/bench` | CLI entry | cli |
| `cmd/bench-helper` | One-shot elevated helper: CA trust and `.test` resolver rule | version |
| `api` | Wire types of the REST/WS API and the Go client for it | binman, paths, supervisor, token |
| `binman` | Download, verify and install catalogued binaries | none |
| `caddy` | Supervise Caddy and load its config through the admin API | binman, portowner, supervisor |
| `cli` | `bench` command tree (cobra) | api, desktop, paths, setup, version |
| `desktop` | Open editors, terminals and database apps | api |
| `dnsstub` | Loopback DNS answering `*.test` | none |
| `doctor` | Machine diagnostics (ports, Herd, DNS, trust) | api, portowner, supervisor |
| `dotenv` | Read and edit Laravel `.env` files | none |
| `elevate` | Find `bench-helper` and run it with UAC/osascript/pkexec | version |
| `mail` | Mailpit API proxy, attachment save, live event relay | api |
| `newapp` | Create a Laravel app as one cancellable task | api, binman, desktop, dotenv, phppool, registry, services, sites, supervisor, tasks |
| `paths` | Every filesystem location under the Bench home | none |
| `phppool` | One FastCGI pool per PHP channel (php-fpm or php-cgi) | api, binman, supervisor |
| `portowner` | Name the process holding a TCP port | none |
| `registry` | SQLite store: sites, services, settings, migrations | none |
| `server` | HTTP and WebSocket handlers, token and Host check | api, binman, desktop, doctor, dotenv, newapp, paths, registry, services, sitedelete, sites, tasks, updates, version |
| `services` | Service instances (MySQL, Postgres, Valkey, Mailpit, ...) | api, binman, desktop, registry, sites, supervisor, tasks |
| `setup` | Undo the machine setup through the helper, with or without a daemon | elevate |
| `sitedelete` | Delete a site and what Bench made for it, as a task | api, desktop, dotenv, mail, services, sites, supervisor, tasks |
| `sites` | Turn the registry into PHP pools and a Caddy config | api, caddy, dotenv, registry, version |
| `supervisor` | Long-lived children (restart, health, logs) and one-shot `Exec` | none |
| `tasks` | Long operations for the Activity view, with cancel | api, binman |
| `token` | API token file | none |
| `updates` | Ask GitHub for the latest release | none |
| `version` | Build version and version comparison | none |

**Direction.** Leaves (no internal imports): `binman`, `dnsstub`, `dotenv`, `paths`, `portowner`, `registry`, `supervisor`, `token`, `updates`, `version`. Above them sit `api` (types), then the single-concern managers (`caddy`, `phppool`, `tasks`, `desktop`, `doctor`, `mail`, `sites`), then the managers that compose others (`services`, `newapp`, `sitedelete`), then `server` and `cli`, then the three commands. There are no import cycles.

Imports that run against that direction:

- `api/api.go:24` and `:26`: the wire-types package imports `binman` and `supervisor` because `api.Event` and `api.Task` embed `binman.Progress` and `supervisor.Status` (`api/api.go:50`, `:52`, `:79`). The JSON contract therefore lives partly in two implementation packages.
- `api/api.go:25` and `:27`: the same package holds the client, which imports `paths` and `token` (`Discover`, `api/api.go:427`). Every daemon package that needs a wire type also links the client.
- `tasks/tasks.go:16`: the generic task tracker imports `binman` for `binman.Progress`.
- `services/services.go:31`: `services` imports `sites` (and through it `caddy` and `registry`) for one function, `sites.NormalizeLabel` (`services/services.go:822`).
- `services/services.go:29`: `services` imports `desktop`, the app launcher, for connection URLs and client command lines (`services/services.go:329`).
- `cli/setup.go:11`: the CLI runs the elevated teardown itself when no daemon answers (`cli/setup.go:74`), the one place a client bypasses the API. It is deliberate (uninstallers) and documented in `setup`.

## Shared state

Goroutines named below: "handlers" are `net/http` request goroutines; "monitor" is `supervisor.monitor`, one per child (`supervisor/supervisor.go:204`); "life work" is `lifeWork.Go` (`cmd/benchd/lifework.go:27`), which shutdown waits for at most 10 seconds (`stopGrace`, `:24`). New Laravel app and site delete jobs run as life work (`newapp/newapp.go:116`, `sitedelete/sitedelete.go:112`, wired at `cmd/benchd/main.go:241-242`).

| State | Where | Who reads and writes it | Protection |
|---|---|---|---|
| `Supervisor.procs`, `closed` | `supervisor/supervisor.go:89-90` | Handlers (site changes through `phppool.Ensure`, service start and stop, doctor), life work (`sites.Apply`, newapp jobs), services work, main (`StopAll`) | `Supervisor.mu` |
| `proc.state`, `pid`, `restarts`, `lastErr` | `supervisor/supervisor.go:116-120` | Written by its monitor; read by `Status`, `Statuses`, `Start` callers | `proc.mu` (lock order `Supervisor.mu`, then `proc.mu`) |
| `proc.ready`, `readyErr`, `done` | `supervisor/supervisor.go:110-114` | Monitor signals; `Start`, `Stop`, `StopAll` wait | `sync.Once` and channel close (`readyErr` is set before `ready` closes) |
| `Supervisor.OnEvent` | `supervisor/supervisor.go:85` | Set once in `cmd/benchd/main.go:283` before the first `Start`; called from monitors. On Caddy ready it hands a `sites.Apply` to life work (`cmd/benchd/main.go:276-281`, `:288`) | Written before use; the callback must not block |
| `tasks.Registry.seq`, `tasks` and each entry | `tasks/tasks.go:54-56` | Handlers (install, setup, list, cancel), newapp and sitedelete jobs (life work), services work, `binman` progress (download goroutine) | `Registry.mu`; `OnChange` runs under it |
| `tasks.Registry.Parent` | `tasks/tasks.go:52` | Set once to the daemon's lifetime context (`cmd/benchd/main.go:236`); read by `Begin` in any goroutine; ending it (`endLife`, `cmd/benchd/main.go:334`) cancels every task's context | Written before use, read-only after; cancellation through `context` |
| `server.Server.subs` (event hub) | `server/server.go:71-72` | `/api/events` handlers subscribe and leave; `Publish` from monitors, tasks (under `Registry.mu`), `sites.Apply`, services, `binman` progress, the mail relay | `subMu`; each subscriber has a 64-event buffer and a send that never blocks (a full buffer drops events) |
| Update check and its cached answer | `server/server.go:70` | `/api/updates` handlers | `updMu`, held across the GitHub request |
| `sites.Manager.lastErr`, `everApplied` | `sites/sites.go:71-72` | Handlers (site changes, `List`, `SetFavorite`), life work (restore, after a PHP install, after Caddy becomes ready, newapp and sitedelete jobs) | `Manager.mu`, held across pool starts and the Caddy install |
| Registry rows that site changes write | `sites/sites.go:424`, `:455-459`, `:479-484`, `:525`, `:546-549` | Handlers, newapp and sitedelete jobs (life work) | Not under `Manager.mu`; only the single SQLite connection orders them, and no change spanning two statements runs in a transaction |
| `registry.Registry.db` | `registry/registry.go:22`, `:34` | Every package above that reads or writes the store | `database/sql` with `SetMaxOpenConns(1)`, WAL, `busy_timeout(5000)`; no transactions exposed |
| `services.Manager.phase`, `pending`, `lastErr`, `opsCtx`, `stopOps`, `closed` | `services/services.go:101-106` | Handlers, autostart goroutine (`cmd/benchd/main.go:310`), monitors through `OnEvent` and `Notify`, newapp and sitedelete jobs (life work), mail (through `FromServices`), main (`Close`, `cmd/benchd/main.go:335`, before life work is waited for) | `Manager.mu`, never held across supervisor or install calls |
| `services.Manager.ops` | `services/services.go:108` | `Add` from every detached operation, `Wait` in `Close` | `Add` only under `Manager.mu` after the `closed` check |
| Port choice plus pending creates | `services/services.go:109` | Create and Clone callers | `allocMu` |
| Order of service list events | `services/services.go:110` | Every `publish` caller | `pubMu` |
| `binman.Manager.inflight` and each flight's waiters | `binman/install.go:55` | Install callers (handlers, services, newapp, `sites.Apply`), the download goroutine, `Uninstall` | `Manager.mu`; a flight's `err` is written before its `done` closes |
| `binman.Manager.OnProgress`, `OnInstalled`, `Manifests` | `cmd/benchd/main.go:172`, `:243`, `:252` | Set in main before the API serves; read by downloads | Written before use, read-only after |
| `lifeWork.stopping`, `wg`, `idle`, `deadline` | `cmd/benchd/lifework.go:14-18` | `Go` from `OnInstalled` (handler or download goroutine), the Caddy-ready hook (monitor), newapp and sitedelete `Start` (handlers), main; `Stop` from main (explicit and deferred), whose first call starts one goroutine that waits on `wg` and closes `idle` | `lifeWork.mu`; `idle` closed once; later `Stop` calls share the first deadline |
| Shutdown request | `cmd/benchd/main.go:208-210` | `/api/shutdown` handler closes, main waits | `sync.Once` around `close` |
| `dnsstub.Stub.udp`, `tcp`, `boundUDP` | `dnsstub/dnsstub.go:38-39` | Written by `Start` on main before the serve goroutines start; read by `Stop` | Ordering of the `go` statement; `dns.Server` locks its own shutdown |
| Windows `php-cli.ini` of each channel | `phppool/spec_windows.go:22` | Rewritten by every `Pools.CLI` call (newapp job at `newapp/newapp.go:274`, `/api/php/{channel}` through `Pools.Info`) while php processes started with `PHPRC` read it | Unprotected |
