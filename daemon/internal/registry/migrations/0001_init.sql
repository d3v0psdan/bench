-- Initial schema per PLAN.md §3.4: sites, services (instance model), and
-- global settings. Effective setting = site.overrides[key] ?? settings[key].

CREATE TABLE sites (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,          -- host label: <name>.test
    path        TEXT    NOT NULL,                 -- project root on disk
    kind        TEXT    NOT NULL DEFAULT 'linked',-- 'linked' | 'parked'
    php_version TEXT,                             -- NULL = global default
    secured     INTEGER NOT NULL DEFAULT 0,
    overrides   TEXT    NOT NULL DEFAULT '{}',    -- per-site settings JSON
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE services (
    id         INTEGER PRIMARY KEY,
    service    TEXT    NOT NULL,                  -- e.g. 'mysql', 'redis'
    version    TEXT    NOT NULL,
    name       TEXT    NOT NULL UNIQUE,           -- instance name
    port       INTEGER,
    binary_dir TEXT,
    data_dir   TEXT,
    config     TEXT    NOT NULL DEFAULT '{}',
    autostart  INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
