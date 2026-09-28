# Contributing to Bench

Bench is in alpha: sites, PHP, services and mail work today ([ROADMAP.md](ROADMAP.md) lists what is done and what comes next). Bug reports, design feedback (issues against [PLAN.md](PLAN.md)) and code are all welcome.

## Ground rules

- Read [PLAN.md](PLAN.md) before proposing architectural changes. Some decisions are locked on purpose (Go daemon, Caddy, native binaries with no containers, API-first design); an issue that reopens one needs new evidence.
- Windows, macOS and Linux are all first-class. A feature PR must work on all three or include a documented per-OS strategy. Until then it stays a draft.
- Keep changes small: stdlib before dependencies, the smallest diff that works, no speculative abstractions. A PR that adds a dependency should say why a few lines of code couldn't do the job.

## Dev setup

Prerequisites: Go 1.26+, Node 22+, [Bun](https://bun.sh), Rust stable (for Tauri), and on Windows the WebView2 runtime (preinstalled on Windows 11).

```sh
git clone https://github.com/d3v0psdan/bench && cd bench
cd daemon && go build ./... && go test ./...   # daemon, CLI, helper
cd ../app && bun install && bun run tauri dev  # desktop app
```

`go test -race` needs cgo and a C compiler. Without one (common on Windows), drop `-race` locally and let CI run it on all three OSes.

## Repo layout

- `daemon/`: Go module with `benchd` (daemon), `bench` (CLI), `bench-helper` (elevated one-shot helper)
- `app/`: Tauri 2 + React + TypeScript + shadcn/ui desktop app
- `manifests/`: versioned catalog of downloadable binaries (PHP, MySQL, …) with checksums
- `docs/`: the UI standard and the Laravel Herd coexistence notes
- `PLAN.md`: architecture and decisions; `ROADMAP.md`: done and next

## Pull requests

1. Open or claim an issue first for anything non-trivial.
2. Branch from `main` and keep each PR to one change.
3. `go test ./...` must pass; new non-trivial logic ships with a test.
4. The GUI and CLI talk to the daemon only through its HTTP API. If your feature can't be driven with `curl`, rework it.
5. Changes to the elevated helper, the token-auth API, DNS or certificate handling get extra review, so flag them in the PR description.

## Security

Found a vulnerability? Report it privately as described in [SECURITY.md](SECURITY.md), not in a public issue. Bench runs a local daemon, writes to trust stores and ships an elevated helper, so these reports get priority, and reporters are credited.

## Conduct

Be kind, assume good faith, no harassment. A full Contributor Covenant code of conduct will land with the first public beta.
