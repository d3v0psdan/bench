-- Folders in a parked directory the user unlinked: they stay on disk but
-- are no longer served or listed. Linking the folder again clears it.

CREATE TABLE ignored_sites (
    id         INTEGER PRIMARY KEY,
    path       TEXT    NOT NULL UNIQUE,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
