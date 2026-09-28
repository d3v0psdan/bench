# Roadmap

What Bench does today, and what comes next. The full design and every decision
behind it live in [PLAN.md](PLAN.md); how to use Bench is in
[INSTRUCTIONS.md](INSTRUCTIONS.md).

Order: Phase 0, 1, 2, then 4, then 3 (Phase 4 was moved ahead of Phase 3 on
2026-09-24; Phase 3 is the last phase).

## Done

### Phase 0 and 1: serve sites
- `benchd` daemon (Go) with a localhost REST and WebSocket API, token auth, and a SQLite registry.
- `bench` CLI and the desktop app (Tauri 2 + React), both thin clients of the daemon.
- PHP 8.3, 8.4 and 8.5 side by side, a global default, and per-site pinning.
- Park and link folders, reverse proxies, all served at `https://<name>.test`.
- Caddy with its own local HTTPS authority; `*.test` DNS on Windows, macOS and Linux.
- One elevated helper for the admin-only steps (trust the CA, set up DNS).

### Phase 2: services and mail
- Native MySQL, MariaDB, PostgreSQL, Valkey (Redis), Meilisearch and RustFS (S3), with no Docker.
- Create, start, stop, clone with data, autostart, and the `.env` lines for each instance.
- Mailpit mail catching with a per-app inbox, read in the app.
- `bench db` opens TablePlus, DBeaver or the bundled terminal client.
- Coexists with Laravel Herd: Bench moves to free ports and says so.

### Phase 4, Milestone A: the GUI audit (all 52 findings closed)
- Grouped sidebar, command palette (Ctrl/Cmd+K), site switcher, favorites and keyboard shortcuts.
- Service logs in the app, one click from any failure, with live follow.
- One "Add site" dialog: New Laravel app, Link a project, Park a folder, Proxy a local port.
- Bench writes a site's `.env` (marked `# Bench generated`), with a preview for existing apps.
- Open a site in your editor, terminal or database app; site favicons.
- Mail page with search, attachments and a safe preview.
- Activity tray and page for every long task, with cancel and step-by-step logs.
- Reconnects after a daemon restart without losing your page; tray menu with sites.
- OS notifications, a daily update check (with an off switch), and start at sign-in.
- Uninstallers remove the DNS rule and the trusted CA (Windows, deb, rpm).
- Security review of the API and the elevated helper; Windows installs per machine.

### Phase 4, Milestone B (partly)
- New Laravel app wizard and `bench new`, following `laravel new`: starter kits, auth, Pest,
  Boost, database, npm or bun. Runs as one cancellable task.
- Composer downloaded through the pinned manifests.

## Next: rest of Phase 4

**Milestone B: Laravel tooling**
- `bench php` and `bench composer` using the site's PHP; Node, nvm and fnm detection with `.nvmrc`.
- `bench.yml`: seeds a site on link; the app shows drift and can save back to the file.
- Tinker: a per-site pane and `bench tinker` (needs its own security review).

**Milestone C: reach and breadth**
- MCP server (`bench mcp`), destructive tools behind a setting.
- Sharing through expose, ngrok or cloudflared (`bench share` and a Share button).
- Typesense, and Reverb as a supervised per-site process.
- Legacy PHP 7.4 to 8.0 where builds exist.
- Windows Job Objects so services can't outlive a crashed daemon.
- Our own CI builds of Valkey for macOS and RustFS for Intel Macs.
- Rename the desktop executable from `app.exe` to Bench.

**Before calling Phase 4 done**
- Screenshot pass of every page in both themes.
- Live checks on Linux, and CI green on Windows, macOS and Linux.
- A test run of the per-machine Windows installer.

## Then: Phase 3, debugging (last phase)
- Dumps (`dump()`, `dd()`, Ray) with a live pane, via a small C PHP extension.
- Log viewer with search.
- Xdebug detection and an XHProf profiler.
- Dev runner: a supervised `php artisan dev` per site (queue, pail, Vite with HTTPS).

With Phase 3 done on all three OSes, Bench reaches v1.0.

## Later
- MongoDB, where our PHP builds carry the `mongodb` extension.

## Not planned
- Custom drivers for non-Laravel PHP apps (WordPress and others).
- Forge integration.
