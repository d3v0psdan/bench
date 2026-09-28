package registry

import (
	"database/sql"
	"errors"
	"fmt"
)

// Site is one sites-table row. PHPVersion "" means "use the global
// default" (stored as NULL).
type Site struct {
	ID         int64
	Name       string
	Path       string
	Kind       string // linked | parked | proxy
	PHPVersion string
	Secured    bool
	ProxyTo    string
	Overrides  string
}

// Sites returns all site rows, ordered by name.
func (r *Registry) Sites() ([]Site, error) {
	rows, err := r.db.Query(`
		SELECT id, name, path, kind, COALESCE(php_version, ''), secured, proxy_to, overrides
		FROM sites ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying sites: %w", err)
	}
	defer rows.Close()
	var out []Site
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.Name, &s.Path, &s.Kind, &s.PHPVersion, &s.Secured, &s.ProxyTo, &s.Overrides); err != nil {
			return nil, fmt.Errorf("scanning site: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpsertSite inserts or updates a site by name.
func (r *Registry) UpsertSite(s Site) error {
	_, err := r.db.Exec(`
		INSERT INTO sites (name, path, kind, php_version, proxy_to)
		VALUES (?, ?, ?, NULLIF(?, ''), ?)
		ON CONFLICT(name) DO UPDATE SET
			path = excluded.path,
			kind = excluded.kind,
			php_version = excluded.php_version,
			proxy_to = excluded.proxy_to`,
		s.Name, s.Path, s.Kind, s.PHPVersion, s.ProxyTo)
	if err != nil {
		return fmt.Errorf("upserting site %s: %w", s.Name, err)
	}
	return nil
}

// SetSitePHP pins (or clears, version "") a site's PHP version.
func (r *Registry) SetSitePHP(name, version string) error {
	res, err := r.db.Exec(`UPDATE sites SET php_version = NULLIF(?, '') WHERE name = ?`, version, name)
	if err != nil {
		return fmt.Errorf("setting php for %s: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no site named %q", name)
	}
	return nil
}

// DeleteSite removes a site row by name.
func (r *Registry) DeleteSite(name string) error {
	res, err := r.db.Exec(`DELETE FROM sites WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("deleting site %s: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no site named %q", name)
	}
	return nil
}

// DeleteParkedSettings removes the settings row a parked site gets on
// first isolation, if it has one; a parked site without one is fine.
func (r *Registry) DeleteParkedSettings(name string) error {
	if _, err := r.db.Exec(`DELETE FROM sites WHERE name = ? AND kind = 'parked'`, name); err != nil {
		return fmt.Errorf("deleting settings of %s: %w", name, err)
	}
	return nil
}

// ParkedDirs returns all parked directories, ordered by path.
func (r *Registry) ParkedDirs() ([]string, error) {
	rows, err := r.db.Query(`SELECT path FROM parked_dirs ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("querying parked dirs: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scanning parked dir: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddParkedDir parks a directory (idempotent).
func (r *Registry) AddParkedDir(path string) error {
	if _, err := r.db.Exec(`INSERT OR IGNORE INTO parked_dirs (path) VALUES (?)`, path); err != nil {
		return fmt.Errorf("parking %s: %w", path, err)
	}
	return nil
}

// RemoveParkedDir unparks a directory.
func (r *Registry) RemoveParkedDir(path string) error {
	res, err := r.db.Exec(`DELETE FROM parked_dirs WHERE path = ?`, path)
	if err != nil {
		return fmt.Errorf("unparking %s: %w", path, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%s is not parked", path)
	}
	return nil
}

// Setting returns a global setting ("" when unset).
func (r *Registry) Setting(key string) (string, error) {
	var v string
	err := r.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading setting %s: %w", key, err)
	}
	return v, nil
}

// SetSetting writes a global setting.
func (r *Registry) SetSetting(key, value string) error {
	_, err := r.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("writing setting %s: %w", key, err)
	}
	return nil
}

// IgnoredPaths returns the parked-directory folders that are not served,
// as the caller stored them (the sites package folds case per OS).
func (r *Registry) IgnoredPaths() (map[string]bool, error) {
	rows, err := r.db.Query(`SELECT path FROM ignored_sites`)
	if err != nil {
		return nil, fmt.Errorf("querying ignored sites: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scanning ignored site: %w", err)
		}
		out[p] = true
	}
	return out, rows.Err()
}

// IgnorePath stops serving a folder in a parked directory (idempotent).
func (r *Registry) IgnorePath(path string) error {
	if _, err := r.db.Exec(`INSERT OR IGNORE INTO ignored_sites (path) VALUES (?)`, path); err != nil {
		return fmt.Errorf("ignoring %s: %w", path, err)
	}
	return nil
}

// UnignorePath serves a previously ignored folder again (idempotent).
func (r *Registry) UnignorePath(path string) error {
	if _, err := r.db.Exec(`DELETE FROM ignored_sites WHERE path = ?`, path); err != nil {
		return fmt.Errorf("unignoring %s: %w", path, err)
	}
	return nil
}
