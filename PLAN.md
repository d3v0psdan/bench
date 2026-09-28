# Bench: system design

Bench is an open-source local development environment for Laravel and PHP on Windows, macOS and Linux. It runs PHP, the web server and every service as native binaries, with no containers, and is MIT licensed. This document covers scope, architecture, design decisions and risks; progress is tracked in [ROADMAP.md](ROADMAP.md).

## 1. Scope

A desktop app, a CLI and a background daemon that together provide:

- PHP: several versions side by side (8.3 to 8.5 today, 7.4 to 8.0 later where builds exist) from static-php-cli builds (php.net builds on Windows) with about 60 extensions matched to Laravel Forge's set, a global default, and per-site pinning (`isolate`).
- Sites: a Valet-style park/link model, reverse proxies, `*.test` DNS and local HTTPS.
- Tooling: Composer, a new Laravel app wizard, Node detection (no bundled nvm), sharing through expose/ngrok/cloudflared, Tinker, and an MCP server.
- Services: native MySQL, MariaDB, PostgreSQL, Valkey (Redis), Meilisearch and RustFS (S3) instances, one binary per instance, cloneable with their data. Typesense and Reverb later; MongoDB is not scheduled.
- Mail: local SMTP on :2525 with a per-project inbox.
- Debugging: dumps (`dump()`/`dd()` and Ray first, then Eloquent queries, jobs, Blade views, outgoing HTTP and logs through a PHP extension), a log viewer, Xdebug auto-detection and a profiler.
- A `bench` CLI that can do everything the app does.

## 2. Architecture

```
┌─────────────────────────────────────────────────────────┐
│  bench-app  (Tauri 2 + React + shadcn/ui + Tailwind)    │
│  tray/menubar, Site Manager, Services, Mail, Dumps,     │
│  Logs, Profiler UI: pure client of the daemon API       │
└──────────────┬──────────────────────────────────────────┘
               │ localhost HTTP + WebSocket (token-auth)
┌──────────────┴──────────────────────────────────────────┐
│  benchd  (Go daemon, user-level service)                 │
│  • process supervisor: php-fpm/php-cgi per version,     │
│    Caddy, service instances, Mailpit                    │
│  • site registry (SQLite): park/link/isolate/secure     │
│  • Caddy config generator → live admin API (no restarts)│
│  • embedded DNS stub (*.test → 127.0.0.1, miekg/dns)    │
│  • dump receivers: Ray HTTP :23517 + VarDumper TCP :9912│
│  • REST + WS event stream (logs, dumps, mail, status)   │
│  • binary manager: download/verify PHP + service builds │
└───────┬──────────────────────────────┬───────────────────┘
        │ spawns/monitors              │ one-shot elevation
┌───────┴───────────────┐   ┌──────────┴──────────────────┐
│ managed binaries      │   │ bench-helper (tiny, elevated)│
│ Caddy (router+TLS+CA) │   │ trust-store install, hosts/  │
│ php-fpm / php-cgi ×N  │   │ NRPT/resolver setup, :80/:443│
│ mysqld, postgres,     │   │ (UAC / sudo / polkit)        │
│ redis, meilisearch,   │   └──────────────────────────────┘
│ rustfs, mailpit, ...  │
└───────────────────────┘
        ▲
        │ same HTTP API
┌───────┴───────────────┐
│ bench CLI (Go, same   │
│ repo as daemon)       │
└───────────────────────┘
```

### Components

1. `benchd`, the daemon (Go). Owns everything: site registry, process supervision with health checks and auto-restart, config generation, binary downloads, dump and mail ingestion, and a localhost REST + WebSocket API that the GUI and CLI both consume. Runs as a launchd agent (macOS), a systemd user unit (Linux), or an auto-start user process or Windows service. Sites keep serving when the UI closes.

2. `bench-app`, the GUI (Tauri 2 + React + shadcn/ui). Tauri because of 3 to 15 MB installers against Electron's 100+ MB, the OS webview, and first-class sidecar, tray, autostart and updater plugins. The UI holds no state: it renders the daemon's API and subscribes to its WS event stream (live dumps, mail arrivals, service status, log tails).

3. `bench`, the CLI. Same Go module as the daemon, talks to the same API. Commands: `park/link/unlink/sites`, `use/isolate/php/composer`, `secure/unsecure`, `proxy`, `services:*`, `logs`, `share`, `db`, `tinker`.

4. `bench-helper`, a minimal elevated helper. The only privileged code: installs the root CA into the system and NSS trust stores, configures DNS (per OS, below), and optionally grants :80/:443. Invoked one-shot with UAC/sudo/polkit; everything else runs unprivileged.

5. Managed binaries: downloaded on demand, checksum-verified, stored under `~/.bench/bin/<name>/<version>/`, with data under `~/.bench/data/<service>/<instance>/`.

### Why Go and not C++

Performance was the main requirement, but the daemon is not on the hot path. Requests flow browser → Caddy → php-fpm, and queries flow app → mysqld. Those are native compiled binaries whatever language the daemon is written in. The daemon supervises processes and serves a control API, so its language has next to no effect on performance.

Comparable tools are written in Go (DDEV, Lerd, Buggregator, Mailpit, Caddy, MinIO), because Go gives single static cross-compiled binaries for all three OSes, goroutine-based process supervision, `miekg/dns` for the DNS stub, and, most useful here, certmagic/Caddy importable as libraries. In C++ each of these is a subsystem to build and maintain, with almost no modern prior art in this niche. C++ would cost months and buy nothing measurable.

Where native performance does matter, Bench is native already: static PHP, Caddy, real mysqld and Valkey binaries. If a hot component shows up later (the dump-interception PHP extension, which has to be C anyway, see §3.6), that piece gets written in C/C++. Decision: Go for the daemon, C only for the PHP extension. A C++ daemon would keep the architecture above (with Boost.Asio/cpp-httplib) at roughly 2 to 3 times the timeline.

## 3. Key design decisions

### 3.1 Web server and TLS: Caddy, not nginx
One Caddy binary replaces nginx, the DNS-to-certificate plumbing and mkcert:
- Built-in local CA (smallstep-based): mints per-site certs and installs its root into the system and Firefox trust stores, which covers `bench secure` almost for free.
- On-demand TLS: with wildcard `*.test` DNS, a new site resolves and serves HTTPS on its first request, with no per-site config and no separate `secure` step.
- Admin API on :2019: benchd applies site changes as live JSON config swaps, with no reloads and no config-file templating races.
- The `php_fastcgi` directive handles the FastCGI plumbing per site.

A per-project `Caddyfile.bench` snippet for custom config was planned and then dropped on 2026-09-24, together with the non-Laravel drivers it was paired with.

### 3.2 PHP: static-php-cli builds
- NTS static builds of PHP 8.3 to 8.5 (7.4 to 8.0 later, for legacy apps), cli and fpm SAPIs, with about 60 extensions matched to Forge's set.
- One php-fpm master per installed version on Unix sockets (macOS and Linux). Windows has no fpm, so there it's php-cgi FastCGI pools per version on localhost ports.
- Known static-build limits: users can't `pecl install`, and Windows static builds lack a static `intl`, so the official php.net NTS zip is the Windows fallback if extension coverage falls short. Publish our extension matrix and build through static-php-cli's GitHub Actions.
- Switching versions means changing which fpm pool a site's Caddy route targets (global default plus a per-site pin). `bench php` and `bench composer` proxy to the pinned version by reading the site registry.

### 3.3 DNS for `*.test`
- macOS: write `/etc/resolver/test` pointing at benchd's embedded DNS stub on 127.0.0.1:<port>. No dnsmasq to bundle: the stub is about 50 lines with miekg/dns.
- Windows: an NRPT rule (`Add-DnsClientNrptRule -Namespace ".test" -NameServers 127.0.0.1`) pointing at the same stub. This is cleaner than rewriting the hosts file: wildcards work, there are no rewrite races, and it survives VPNs. Hosts-file writing stays as a documented fallback.
- Linux: a systemd-resolved drop-in routing `~test` to the stub, with a NetworkManager/dnsmasq fallback.

### 3.4 Services
Native binaries, one per instance, no containers:
| Service | Source |
|---|---|
| MySQL 8.4 / 9.7 | official zip / tarballs (Linux builds are tar.xz), all 5 platforms |
| MariaDB 11.4 / 11.8 | official zip / bintar: Windows x64 and Linux x64 only (no macOS or Linux arm64 upstream) |
| PostgreSQL 16 to 18 | theseus-rs/postgresql-binaries (relocatable, all 5 platforms; EDB stopped shipping Linux) |
| Valkey 9.1 (Redis-compatible) | official Linux tarballs; community valkey-windows build on Windows; no macOS build upstream |
| Meilisearch, Mailpit | official single binaries, all 3 OSes |
| RustFS (S3) | official zips; replaces MinIO, whose community binaries were withdrawn in Oct 2025; no Intel macOS build |
| Typesense, Reverb (PHP) | phase 4 |
| MongoDB | future todo, not scheduled |

*Decided 2026-09-24 (Phase 2):* Redis itself publishes no binaries at all,
so Valkey is the Redis service everywhere (`REDIS_*` env vars work as-is).
Platform gaps are filled pragmatically, Windows and Linux first: macOS
Valkey and Intel-macOS RustFS will be our own CI builds hosted on GitHub
Releases; MariaDB on macOS/Linux arm64 is not offered (MySQL covers it).
Downloads verify pinned SHA-256s; provenance per manifest is in
`manifests/README.md`.

Instance model: `{service, version, port, binary_dir, data_dir, config}` rows in SQLite; start/stop/autostart, clone with data, and per-project defaults through `bench.yml`. The manifest of download URLs and SHA-256s is a versioned JSON the daemon fetches, so it can be updated without an app release.

Settings model: one `settings` table (global) plus a JSON `overrides` column on each site row. Effective value = `site.overrides[key] ?? global[key]`. This covers PHP version, package manager, dev-runner autostart and cert options with one resolution function and no config framework.

### 3.5 Mail
Bundle Mailpit (a single Go binary with SMTP, a REST API and WS). Its SMTP default is :1025; Bench uses :2525 so `.env` files written for Laravel Herd work unchanged. The GUI's Mail pane renders Mailpit's REST/WS API in shadcn, with per-project inboxes keyed on the SMTP username (`APP_NAME`). No custom mail code.

### 3.6 Dumps
First step: speak the open protocols. benchd runs Ray HTTP :23517 and Symfony VarDumper TCP :9912 receivers, rendered live in the GUI over WS. This works today with `ray()` and `VAR_DUMPER_FORMAT=server`, and needs no app changes for Laravel's `dump()` once Caddy's fastcgi params set the env vars. Spatie's Ray desktop app binds :23517 itself; on a bind failure Bench names the Ray app and offers a port override (`bench doctor` covers this).

Second step: a small C PHP extension, loaded through `auto_prepend_file`/ini injection, that registers query, job, view and HTTP listeners. This is the one component that has to be native code. Buggregator's Go receivers are the reference implementation for the server side.

### 3.7 Debugging and profiling
- Bundle Xdebug per PHP version. Xdebug auto-detection: benchd polls for a listener on :9003; when an IDE listens, it injects `xdebug.mode=debug` through the env for subsequent requests and drops it when the listener goes away.
- Profiler: XHProf (the longxinH fork, which has Windows DLLs and PHP 8 support) compiled into our static builds, with a flamegraph and call-tree UI in the app. SPX where available (macOS and Linux) as the nicer option, XHProf as the cross-platform floor.

### 3.8 Log viewer
benchd tails `storage/logs/*` per site (fsnotify), parses the Laravel/Monolog format and streams it over WS, with full-text search on the server side. Plain Go and React work, no third-party service.

### 3.9 JS runtimes and the dev runner
Don't bundle nvm. Detect the system Node/bun/nvm/fnm and integrate with it (per-site version pinning through `.nvmrc` and PATH shims). Working with the user's own Node setup beats shipping a second nvm next to it.

Package manager (npm/bun/pnpm/yarn): no preference plumbing. Laravel 13.16's `NodePackageManager` already resolves it from the lockfile (bun.lock → pnpm → yarn → npm), and `laravel new` takes `--bun`/`--pnpm` flags. Bench needs one global setting, the default package manager for new projects, passed to the creation wizard. Per site, the lockfile decides.

Dev runner: Laravel 13.16 added `php artisan dev` to the framework. It runs queue:listen, pail and vite through concurrently, with `--except`/`--only` filters and userland `DevCommands::register()` extensions. Bench doesn't rebuild this. Per site, benchd spawns `php artisan dev --except=server` (Caddy is the server) as a supervised child, streams its output to the GUI over WS, and offers start, stop and auto-start. For apps older than 13.16 it falls back to the site's composer `dev` script minus serve, or plain `<pm> run dev`.

Vite HMR behind `https://site.test`: laravel-vite-plugin auto-detects Herd and Valet TLS but not Bench's, so benchd injects `VITE_DEV_SERVER_KEY`/`VITE_DEV_SERVER_CERT` (Caddy-minted cert paths) into the dev runner's env. HTTPS and wss HMR then work without `vite.config.js` edits. Stale `public/hot` files get cleaned up when the runner stops.

### 3.10 Sharing, Forge, MCP
- Sharing: shell out to a user-installed `expose`/`ngrok`/`cloudflared` (detect and configure) rather than bundling a tunnel client.
- Forge integration: *dropped 2026-09-24*, not planned.
- MCP server in benchd (site info, service control, logs and dumps as MCP tools) for editor and assistant integrations. It's cheap because the REST API already exists.

### 3.11 Repo and distribution
- Monorepo: `daemon/` (Go: benchd, CLI, helper), `app/` (Tauri + React), `manifests/` (binary catalog), `ext/` (PHP extension, Phase 3).
- License: MIT. CI: GitHub Actions matrix builds; installers (MSI/NSIS, DMG, AppImage/deb/rpm) with the daemon bundled as a Tauri sidecar; the Tauri updater for the app, self-update for the daemon.
- *Decided 2026-09-24 (Phase 2):* the v0.1 beta ships unsigned installers (users see SmartScreen/Gatekeeper warnings). Signing and macOS notarization are wired into `.github/workflows/release.yml` and switch on once the `APPLE_*` and `WINDOWS_CERTIFICATE` secrets exist.
- `bench db` per OS: TablePlus (Windows: `%LOCALAPPDATA%` or Program Files, opened through its URL scheme; macOS: `/Applications/TablePlus.app`; Linux: `tableplus` on PATH), then DBeaver (same locations; macOS uses its own launcher so a running DBeaver receives the connection), then the terminal client bundled with the instance (`mysql`, `mariadb`, `psql`, `valkey-cli`). `--url` prints the connection URL for any other client.
- Trademark: "Laravel" is a protected mark, so the product ships as Bench with "for Laravel" as a descriptor.

## 4. Roadmap

Phases, what is done and what comes next: [ROADMAP.md](ROADMAP.md).

*Decided 2026-09-24:* Phase 4 runs before Phase 3, and Phase 3 is the last phase. Dropped: custom drivers for non-Laravel PHP apps (WordPress etc.) and Forge integration. The dump PHP extension moved to Phase 3, next to the Dumps pane it feeds.

*Phase 4 scope, decided 2026-09-24:*
- Navigation (the target map, including the Phase 3 panes Dumps, Logs and Dev): a collapsible grouped left sidebar replaces the top tab row. Machine scope: Overview; Local (Sites, Services, Runtimes); Inspect (Mail, Dumps, Activity); Settings. PHP is renamed Runtimes so Node detection can join it. On a site page the sidebar switches to the site's pages (Overview, Logs, Dumps, Dev, Settings) with an "All sites" link back.
- `bench.yml`: seeds the site on link; afterwards the GUI shows drift between the site and its file, and Bench writes the file only on an explicit "Save to bench.yml".
- Composer: `composer.phar` is downloaded through `manifests/` with a pinned SHA-256; new projects use `composer create-project` (starter kits included), so there is no Laravel installer binary.
- MCP: read-only and reversible tools by default; destructive tools (delete a service, uninstall PHP, remove a site) sit behind a setting that is off by default.
- Update check: on by default, at most one GitHub request a day, off switch in Settings.
- OS notifications: service crash on, install finished on, new mail off by default; each has a toggle.
- MongoDB: not in Phase 4 (owner, 2026-09-24). Future todo: add it where our PHP builds carry the `mongodb` extension, gaps documented here.
- Legacy PHP 7.4 to 8.0: shipped wherever upstream builds exist, Windows first (php.net archives), gaps documented here.
- `Caddyfile.bench`: dropped (§3.1).
- Uninstaller hooks: all three installers (NSIS, deb, rpm) undo the setup.
- Windows installs per machine (decided 2026-09-25, owner; security review M1): NSIS `installMode: perMachine` puts Bench in Program Files, so the elevated `bench-helper.exe` sits where only administrators can write. Install and update ask for admin rights once. The helper records the CA it trusted in HKLM, so an uninstall run under another admin's profile still removes it.

*Decided 2026-09-25 (owner):* the new-site wizard and Composer move forward, ahead of the rest of Milestone A. "Add site" is one stepper dialog (New Laravel app, Link, Park, Proxy). New apps ask the Laravel installer's questions in its order (laravel/installer `NewCommand.php`), npm or bun, and go into a "projects folder" setting (default `~/Bench`) that Bench parks. SQL Server is not offered: Bench has no service for it. Pest 5's `--drift` rejects the installer's call, so that one step (restyling the example tests) is best-effort. Two more differences from `laravel new` are deliberate. The starter kits' `post-create-project` installer hook (`php artisan install:features`, an interactive picker of which kit features to keep) isn't run, so a new app keeps every feature and the user can run it later. And `bench db` launches the database app from the CLI process rather than through `POST /api/services/{name}/open`, because its terminal-client fallback needs the CLI's own terminal (both run as the same user, so the round trip gains nothing).

## 5. Risks

| Risk | Mitigation |
|---|---|
| Windows static PHP extension gaps (no static intl) | official php.net NTS zip fallback path on Windows |
| Redis has no official binaries (any OS) | ship Valkey everywhere; community valkey-windows build on Windows, own CI build planned for macOS |
| MinIO withdrew its community binaries (2025) | RustFS as the S3 service |
| Laravel Herd or another local server already holds :443 or :2525 | `bench doctor` names the holder; services and mail move to free ports ([docs/herd-coexistence.md](docs/herd-coexistence.md)) |
| Trust-store/DNS setup needs elevation | single tiny helper, one-shot UAC/sudo, everything else unprivileged |
| Binary hosting bandwidth (PHP/MySQL downloads) | download from upstream vendors directly + manifest checksums; mirror only our PHP builds via GitHub Releases |
| Query, job and view dumps need a PHP extension | phase it: open Ray/VarDumper protocols first (90% of the value), C extension later |
| "Laravel" trademark | name the product Bench early |
