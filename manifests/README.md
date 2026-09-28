# manifests

Versioned JSON catalog of downloadable binaries with download URLs and
pinned checksums. benchd embeds a snapshot of these files at build time
(`daemon/internal/binman/manifests/`, kept in sync by a unit test) and
verifies every download against them before extracting anything.

## Checksum provenance

- **caddy.json:** SHA-512 copied verbatim from the official
  `caddy_<version>_checksums.txt` release asset (Caddy publishes SHA-512,
  not SHA-256).
- **php.json, Windows builds:** SHA-256 copied verbatim from
  `https://windows.php.net/downloads/releases/releases.json`. URLs point at
  `releases/archives/`, which keeps every file permanently (the plain
  `releases/` path breaks when a new patch ships).
- **php.json, Linux/macOS builds** (static-php-cli, dl.static-php.dev):
  the vendor publishes **no checksums**, so these SHA-256s are
  compute-and-pin: hashed once at manifest-authoring time from the
  downloaded artifacts (2026-07-01). Trust-on-first-download; any upstream
  tampering after pinning fails verification on every install.
- **composer.json** (2.10.3): one platform-independent `composer.phar`,
  run by Bench's own PHP. SHA-256 from the vendor's
  `composer.phar.sha256sum` next to the file on getcomposer.org, and the
  downloaded phar re-hashed to confirm (2026-09-25).
- **mysql.json** (8.4.11, 9.7.2): Oracle publishes only MD5 on
  dev.mysql.com. Every artifact was downloaded from `cdn.mysql.com`, its
  MD5 matched against the value on the vendor download page, and the
  SHA-256 computed from that same file is pinned (2026-09-24).
- **mariadb.json** (11.4.13, 11.8.9): SHA-256 copied from the vendor
  `sha256sums.txt` next to each file on `archive.mariadb.org` (permanent
  URLs); all four artifacts downloaded and re-hashed to confirm
  (2026-09-24). Only Windows x64 and Linux x86_64 exist upstream.
- **postgresql.json** (16.15, 17.11, 18.6): SHA-256 from the per-asset
  `.sha256` files of theseus-rs/postgresql-binaries, cross-checked against
  the GitHub release asset digests; all 15 artifacts downloaded and
  re-hashed (2026-09-24). theseus tags releases with an extra `.0` (e.g.
  `18.6.0`); the manifest `version` is the upstream PostgreSQL version
  (`18.6`) while URLs keep the tag.
- **valkey.json** (9.1.2): Linux SHA-256 from the vendor `.sha256` files
  on download.valkey.io (jammy builds, for older glibc). Windows uses the
  community valkey-windows msys2 build (runtime DLLs bundled), pinned from
  the GitHub release asset digest. No macOS build exists upstream. All
  three artifacts re-hashed (2026-09-24).
- **meilisearch.json** (1.54.0): GitHub release asset digests (the vendor
  publishes no checksum file); linux-amd64 re-hashed to confirm
  (2026-09-24). Community edition, not the `-enterprise` assets.
- **rustfs.json** (1.0.0): vendor `SHA256SUMS` release asset, cross-checked
  against the GitHub asset digests; Windows and Linux x86_64 re-hashed
  (2026-09-24). No Intel macOS build exists upstream.
- **mailpit.json** (1.31.2): GitHub release asset digests (no vendor
  checksum file); Windows and Linux amd64 re-hashed (2026-09-24).

## Schema (v1)

```json
{
  "schema": 1,
  "name": "php",
  "builds": [
    {
      "version": "8.4.22",         // exact version installed
      "channel": "8.4",            // what users ask for (bench php:install 8.4)
      "os": "linux",               // GOOS: windows | darwin | linux
      "arch": "amd64",             // GOARCH: amd64 | arm64
      "downloads": [
        { "url": "https://…", "sha256": "…", "archive": "tar.gz" }
      ]
    }
  ]
}
```

Each download pins exactly one of `sha256`/`sha512` and an `archive`
format (`zip`, `tar.gz`, `tar.xz` or `raw`). URLs must be https.

Two optional download fields:

- `strip`: number of leading path components dropped from every entry, so
  the install dir directly holds `bin/` and friends (MySQL, MariaDB,
  PostgreSQL and Valkey archives wrap everything in one top-level folder,
  so they use `1`). Omitted when 0. Always determined by listing the real
  archive, never guessed.
- `file`: only for `archive: "raw"`, where the download is a bare
  executable rather than an archive. Names the installed file, e.g.
  `meilisearch.exe` on Windows and `meilisearch` elsewhere.

Archives may contain symlinks (Linux/macOS MySQL, MariaDB and PostgreSQL
builds do, for versioned shared libraries and legacy tool names). They
must resolve inside the install dir; no Windows archive in the catalog
contains any.

The same channel can map to different versions per OS (e.g. php.net and
static-php-cli lag each other by a patch release).
