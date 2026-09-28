-- Phase 1: parked directories (Valet model: every subdir serves), proxy
-- sites, and the global PHP default.

CREATE TABLE parked_dirs (
    id         INTEGER PRIMARY KEY,
    path       TEXT    NOT NULL UNIQUE,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- kind gains 'proxy'; proxy sites reverse-proxy the whole host to a local
-- address instead of serving files.
ALTER TABLE sites ADD COLUMN proxy_to TEXT NOT NULL DEFAULT '';

INSERT INTO settings (key, value) VALUES ('php.default', '8.4');
