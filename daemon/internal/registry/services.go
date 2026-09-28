package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrServiceNotFound reports a service instance name with no row.
var ErrServiceNotFound = errors.New("no such service instance")

// ErrServiceExists reports a create against a name already taken.
var ErrServiceExists = errors.New("service instance already exists")

// Service is one services-table row: a managed instance of a service
// binary (mysql, postgresql, ...) with its own port and data directory.
type Service struct {
	ID        int64
	Kind      string // column "service": mysql | mariadb | postgresql | ...
	Channel   string // column "version": the catalog channel, e.g. "8.4"
	Name      string
	Port      int
	BinaryDir string // exact installed build this instance runs
	DataDir   string
	Config    string // JSON: driver extras (secondary ports, credentials)
	Autostart bool
}

const serviceColumns = `id, service, version, name, COALESCE(port, 0), COALESCE(binary_dir, ''), COALESCE(data_dir, ''), config, autostart`

func scanService(row interface{ Scan(...any) error }) (Service, error) {
	var s Service
	err := row.Scan(&s.ID, &s.Kind, &s.Channel, &s.Name, &s.Port, &s.BinaryDir, &s.DataDir, &s.Config, &s.Autostart)
	return s, err
}

// Services returns all service instances, ordered by name.
func (r *Registry) Services() ([]Service, error) {
	rows, err := r.db.Query(`SELECT ` + serviceColumns + ` FROM services ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying services: %w", err)
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning service: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Service returns one instance by name, or ErrServiceNotFound.
func (r *Registry) Service(name string) (Service, error) {
	s, err := scanService(r.db.QueryRow(`SELECT `+serviceColumns+` FROM services WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return Service{}, fmt.Errorf("%w: %q", ErrServiceNotFound, name)
	}
	if err != nil {
		return Service{}, fmt.Errorf("reading service %s: %w", name, err)
	}
	return s, nil
}

// InsertService adds a new instance; a taken name is ErrServiceExists.
func (r *Registry) InsertService(s Service) error {
	config := s.Config
	if config == "" {
		config = "{}"
	}
	_, err := r.db.Exec(`
		INSERT INTO services (service, version, name, port, binary_dir, data_dir, config, autostart)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Kind, s.Channel, s.Name, s.Port, s.BinaryDir, s.DataDir, config, s.Autostart)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("%w: %q", ErrServiceExists, s.Name)
		}
		return fmt.Errorf("inserting service %s: %w", s.Name, err)
	}
	return nil
}

// SetServiceBinaryDir records a (re)installed build for an instance.
func (r *Registry) SetServiceBinaryDir(name, dir string) error {
	return r.updateService(name, `UPDATE services SET binary_dir = ? WHERE name = ?`, dir, name)
}

// SetServiceConfig replaces an instance's config JSON.
func (r *Registry) SetServiceConfig(name, config string) error {
	return r.updateService(name, `UPDATE services SET config = ? WHERE name = ?`, config, name)
}

// SetServiceAutostart toggles whether the instance starts with benchd.
func (r *Registry) SetServiceAutostart(name string, enabled bool) error {
	return r.updateService(name, `UPDATE services SET autostart = ? WHERE name = ?`, enabled, name)
}

// DeleteService removes an instance row.
func (r *Registry) DeleteService(name string) error {
	return r.updateService(name, `DELETE FROM services WHERE name = ?`, name)
}

func (r *Registry) updateService(name, query string, args ...any) error {
	res, err := r.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("updating service %s: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %q", ErrServiceNotFound, name)
	}
	return nil
}
