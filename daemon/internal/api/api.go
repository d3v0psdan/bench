// Package api defines benchd's wire types and a Go client for its
// localhost HTTP API. The CLI (and anything else) talks to the daemon
// exclusively through this, never the database.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/paths"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
	"github.com/d3v0psdan/bench/daemon/internal/token"
)

// DefaultPort is benchd's default listen port ("BENCH" on a phone keypad).
const DefaultPort = 23624

// Status is the payload of GET /api/status and of "status" WS events.
type Status struct {
	Version       string    `json:"version"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	PID           int       `json:"pid"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
}

// Event is the envelope streamed over the /api/events WebSocket. Type
// selects which payload field is set: "status" | "download" | "sites" |
// "process" | "services" | "tasks" | "mail" (Mailpit's own event JSON,
// relayed verbatim: {"Type":"new","Data":{...message summary}}).
type Event struct {
	Type     string             `json:"type"`
	Status   *Status            `json:"status,omitempty"`
	Download *binman.Progress   `json:"download,omitempty"`
	Sites    []Site             `json:"sites,omitempty"`
	Process  *supervisor.Status `json:"process,omitempty"`
	Services []Service          `json:"services,omitempty"`
	Tasks    []Task             `json:"tasks,omitempty"`
	Mail     json.RawMessage    `json:"mail,omitempty"`
}

// Task states.
const (
	TaskRunning   = "running"
	TaskDone      = "done"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"
)

// Task is one long-running operation (GET /api/tasks and "tasks" WS
// events, which always carry the full list).
type Task struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`  // binary.install | service.create | service.clone | setup | site.new
	Title string `json:"title"` // "Installing PHP 8.4"
	// Target is what the task works on: "php/8.4" for installs, the
	// instance name for services.
	Target string `json:"target"`
	Phase  string `json:"phase,omitempty"` // downloading | initializing | cancelling ...
	State  string `json:"state"`
	// Cancellable is true while POST /api/tasks/{id}/cancel would work.
	Cancellable bool             `json:"cancellable"`
	Download    *binman.Progress `json:"download,omitempty"`
	LogFile     string           `json:"log_file,omitempty"` // the task's own output, when it keeps one
	Error       string           `json:"error,omitempty"`
	StartedAt   time.Time        `json:"started_at"`
	EndedAt     *time.Time       `json:"ended_at,omitempty"`
}

// Site is one served host (GET /api/sites and "sites" WS events).
type Site struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	URL       string `json:"url"`
	Path      string `json:"path,omitempty"`
	Kind      string `json:"kind"`                 // linked | parked | proxy
	PHP       string `json:"php,omitempty"`        // effective version
	PHPPinned string `json:"php_pinned,omitempty"` // "" = global default
	ProxyTo   string `json:"proxy_to,omitempty"`
	Error     string `json:"error,omitempty"`
	// Favorite sites lead lists; ParkedIn is the parked directory a
	// parked site comes from.
	Favorite bool   `json:"favorite,omitempty"`
	ParkedIn string `json:"parked_in,omitempty"`
	// AppName is the app's APP_NAME from its .env: the inbox its mail
	// files under (MAIL_USERNAME="${APP_NAME}").
	AppName string `json:"app_name,omitempty"`
	// EnvPorts are the local service ports the app's .env points at
	// (database, Redis, mail, search, S3), so clients can tell which
	// Bench services it uses.
	EnvPorts []int `json:"env_ports,omitempty"`
	// PathMissing: a linked site's folder was moved or deleted; relink it
	// to the new place or remove it.
	PathMissing bool `json:"path_missing,omitempty"`
}

// Mutation request bodies for the /api/sites/* endpoints.
type (
	PathRequest struct {
		Path string `json:"path"`
	}
	LinkRequest struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	NameRequest struct {
		Name string `json:"name"`
	}
	FavoriteRequest struct {
		Name     string `json:"name"`
		Favorite bool   `json:"favorite"`
	}
	SitePHPRequest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	ProxyRequest struct {
		Name   string `json:"name"`
		Target string `json:"target"`
	}
	SettingRequest struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
)

// Service is one managed service instance (GET /api/services and
// "services" WS events).
type Service struct {
	Name    string         `json:"name"`
	Service string         `json:"service"` // mysql | mariadb | postgresql | valkey | meilisearch | rustfs | mailpit
	Channel string         `json:"channel"` // catalog channel, e.g. "8.4"
	Version string         `json:"version"` // exact build, e.g. "8.4.11"
	Port    int            `json:"port"`
	Ports   map[string]int `json:"ports,omitempty"` // secondary ports by role (http, console)
	// State: the supervisor state (starting | running | backoff |
	// stopped | failed) or a lifecycle phase (installing | initializing |
	// cloning | stopping).
	State     string   `json:"state"`
	PID       int      `json:"pid,omitempty"`
	Autostart bool     `json:"autostart"`
	DataDir   string   `json:"data_dir"`
	BinDir    string   `json:"bin_dir,omitempty"` // installed build (bundled clients live here)
	LogFile   string   `json:"log_file"`          // the process output (GET /api/services/{name}/logs)
	Env       []string `json:"env"`               // .env lines that connect a Laravel app
	Notice    string   `json:"notice,omitempty"`  // e.g. why the port differs from the default
	// Connecting: URL is the client connection URL (databases), Client
	// the bundled terminal client's command line, ConsoleURL a web
	// console (RustFS, Meilisearch) and ConsoleLogin what it asks for.
	URL          string       `json:"url,omitempty"`
	Client       string       `json:"client,omitempty"`
	ConsoleURL   string       `json:"console_url,omitempty"`
	ConsoleLogin []Credential `json:"console_login,omitempty"`
	// UIAuth is Mailpit's "user:password" for its web UI and API. In
	// process only (the mail proxy adds it); never sent to clients.
	UIAuth string `json:"-"`
	Error  string `json:"error,omitempty"`
}

// SiteDeletePlan is GET /api/sites/{name}/delete: everything deleting the
// site can clean up, so the confirmation lists exactly that. Empty fields
// don't apply to this site.
type SiteDeletePlan struct {
	Site     Site          `json:"site"`
	Folder   string        `json:"folder,omitempty"`   // project folder (none for a proxy)
	Database *SiteDatabase `json:"database,omitempty"` // its database on a Bench instance
	Service  string        `json:"service,omitempty"`  // an instance made for this site alone
	MailTag  string        `json:"mail_tag,omitempty"` // its inbox (APP_NAME)
	LogFile  string        `json:"log_file,omitempty"` // its new-app creation log
}

// SiteDatabase is a site's database: DB_DATABASE on the Bench instance its
// .env points at.
type SiteDatabase struct {
	Service string `json:"service"`
	Name    string `json:"name"`
}

// SiteDeleteRequest is POST /api/sites/{name}/delete: which parts of the
// plan to remove. The site always stops being served.
type SiteDeleteRequest struct {
	Folder   bool `json:"folder"`   // move the project folder to the OS trash
	Database bool `json:"database"` // drop its database
	Service  bool `json:"service"`  // delete its own instance and data
	Mail     bool `json:"mail"`     // delete its inbox's messages
	Log      bool `json:"log"`      // delete its creation log
}

// NewSiteRequest is POST /api/sites/new: a Laravel app created the way
// `laravel new` does it, answer for answer.
type NewSiteRequest struct {
	Name       string `json:"name"`
	Dir        string `json:"dir,omitempty"` // parent folder; "" = the projects.dir setting
	StarterKit bool   `json:"starter_kit"`
	Stack      string `json:"stack"`          // blade | react | svelte | vue | livewire
	Auth       string `json:"auth,omitempty"` // laravel | workos (starter kits only)
	// ClassComponents picks the Livewire kit's class components over
	// single-file ones.
	ClassComponents bool   `json:"class_components,omitempty"`
	Teams           bool   `json:"teams,omitempty"`
	Testing         string `json:"testing"` // pest | phpunit
	Boost           bool   `json:"boost"`
	Database        string `json:"database"`          // sqlite | mysql | mariadb | pgsql
	Service         string `json:"service,omitempty"` // Bench instance to use; "" creates one
	Migrate         bool   `json:"migrate"`
	// PackageManager runs install and build: npm | bun; "" skips both.
	PackageManager string `json:"package_manager,omitempty"`
}

// Tool is a command-line tool Bench looked for on PATH (GET /api/tools);
// Path is "" when it isn't installed.
type Tool struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

// UpdateInfo is GET /api/updates: this Bench, the newest published
// release, and whether it is newer. Enabled is the automatic daily check.
type UpdateInfo struct {
	Enabled   bool       `json:"enabled"`
	Current   string     `json:"current"`
	Latest    string     `json:"latest,omitempty"`
	URL       string     `json:"url,omitempty"`
	Newer     bool       `json:"newer"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// CLIInfo is GET /api/cli: where `bench` resolves on PATH ("" = not on
// PATH) and the copy shipped beside benchd ("" = none, a dev build).
type CLIInfo struct {
	OnPath  string `json:"on_path,omitempty"`
	Bundled string `json:"bundled,omitempty"`
}

// PHPInfo is GET /api/php/{channel}: an installed PHP's details for its
// sheet. Ini is "" where settings are compiled in (static Unix builds).
type PHPInfo struct {
	Channel    string   `json:"channel"`
	Version    string   `json:"version"`
	Dir        string   `json:"dir"`
	Server     string   `json:"server"`
	Ini        string   `json:"ini,omitempty"`
	Extensions []string `json:"extensions"`
}

// Credential is one labelled value a console asks for.
type Credential struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

// Editor is a code editor Bench can open a site in (GET /api/editors).
type Editor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

// OpenSiteRequest is POST /api/sites/{name}/open: In is "editor",
// "terminal" or "database"; Editor picks one for "editor" (default: the
// editor.default setting).
type OpenSiteRequest struct {
	In     string `json:"in"`
	Editor string `json:"editor,omitempty"`
}

// EnvRequest is POST /api/sites/{name}/env: KEY=value lines to set in the
// site's .env. DryRun reports the changes without writing.
type EnvRequest struct {
	Lines  []string `json:"lines"`
	DryRun bool     `json:"dry_run"`
}

// EnvResult is what an EnvRequest changed (or would change) in Path.
type EnvResult struct {
	Path    string      `json:"path"`
	Changes []EnvChange `json:"changes"`
}

// EnvChange is one key set: Old is "" when the key was absent.
type EnvChange struct {
	Key   string `json:"key"`
	Old   string `json:"old"`
	New   string `json:"new"`
	Added bool   `json:"added"`
}

// Opened names the app something was opened in.
type Opened struct {
	App string `json:"app"`
}

// LogTail is the end of a log file (GET /api/services/{name}/logs).
// Size is the file's length when read: pass it as ?from= to the follow
// socket so no line is missed or repeated.
type LogTail struct {
	Path string `json:"path"`
	Text string `json:"text"`
	Size int64  `json:"size"`
}

// LogChunk is one follow-socket message: text appended since the last
// chunk, or Reset when the file shrank (rotated or truncated) and the
// client should clear what it shows before appending Text.
type LogChunk struct {
	Text  string `json:"text"`
	Reset bool   `json:"reset,omitempty"`
}

// ServiceType is one creatable service kind (GET /api/services/catalog).
type ServiceType struct {
	Service     string   `json:"service"`
	Label       string   `json:"label"`
	Channels    []string `json:"channels"` // installable on this platform, catalog order
	DefaultPort int      `json:"default_port"`
	Singleton   bool     `json:"singleton,omitempty"` // at most one instance (mailpit)
}

// Service request bodies.
type (
	CreateServiceRequest struct {
		Service   string `json:"service"`
		Channel   string `json:"channel"` // "" = newest in the catalog
		Name      string `json:"name"`    // "" = the service name
		Port      int    `json:"port"`    // 0 = default, or the next free one
		Autostart bool   `json:"autostart"`
	}
	CloneServiceRequest struct {
		Name string `json:"name"` // the new instance
	}
	AutostartRequest struct {
		Enabled bool `json:"enabled"`
	}
)

// Check is one `bench doctor` finding (GET /api/doctor).
type Check struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail"` // what's wrong, in one or two sentences
	// Steps are the fix, in order, one action each; empty when ok or when
	// there is nothing to do. Backticked spans are commands.
	Steps []string `json:"steps,omitempty"`
	// Action names the button a GUI offers for the fix: "setup" (run the
	// HTTPS and DNS setup) or "recheck" (fix it outside Bench, then run
	// the checks again). Empty when there is nothing to press.
	Action string `json:"action,omitempty"`
}

// BinaryInfo describes one installable build for this platform.
type BinaryInfo struct {
	Name      string `json:"name"`
	Channel   string `json:"channel"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	// Dir is where the build lives, or will live once installed.
	Dir string `json:"dir"`
	// Sources are the hosts it downloads from, for the install
	// confirmation ("windows.php.net"); every download is checksummed.
	Sources []string `json:"sources"`
	// Size is the installed build's size on disk in bytes (0 when not
	// installed), so an uninstall can say what it frees.
	Size int64 `json:"size,omitempty"`
}

// InstallRequest is the body of POST /api/binaries/install.
type InstallRequest struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
}

// SetupRequest is the body of POST /api/setup (one elevation prompt).
type SetupRequest struct {
	DNS   bool `json:"dns"`   // install the .test resolver rule
	Trust bool `json:"trust"` // install the local CA into trust stores
}

// SetupResponse reports what the elevated helper accomplished.
type SetupResponse struct {
	Messages []string `json:"messages"`
}

// ErrNotRunning reports that benchd is not reachable.
var ErrNotRunning = errors.New("benchd is not running")

// Client calls the daemon API.
type Client struct {
	Addr  string // host:port
	token string
	http  *http.Client
	slow  *http.Client // no timeout: long calls (installs) bounded by ctx
}

// NewClient returns a client for the daemon at addr (host:port).
func NewClient(addr, tok string) *Client {
	return &Client{
		Addr:  addr,
		token: tok,
		http:  &http.Client{Timeout: 5 * time.Second},
		slow:  &http.Client{},
	}
}

// Discover locates a running daemon via the addr file and token file under
// the bench home. Returns ErrNotRunning when no daemon has written an
// address.
func Discover() (*Client, error) {
	addrFile, err := paths.AddrFile()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(addrFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotRunning
		}
		return nil, fmt.Errorf("reading daemon address: %w", err)
	}

	tokenFile, err := paths.TokenFile()
	if err != nil {
		return nil, err
	}
	tok, err := token.Load(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("loading api token: %w", err)
	}
	return NewClient(strings.TrimSpace(string(b)), tok), nil
}

// Status fetches the daemon status. A connection failure maps to
// ErrNotRunning (e.g. a stale addr file after a crash).
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var st Status
	if err := c.get(ctx, "/api/status", &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Shutdown asks the daemon to exit gracefully.
func (c *Client) Shutdown(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.Addr+"/api/shutdown", nil)
	if err != nil {
		return err
	}
	resp, err := c.do(c.http, req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Binaries lists the installable catalog for this platform.
func (c *Client) Binaries(ctx context.Context) ([]BinaryInfo, error) {
	var out []BinaryInfo
	if err := c.get(ctx, "/api/binaries", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InstallBinary installs name/channel, blocking until the daemon finishes
// (PHP downloads take minutes: bound with ctx, not a client timeout).
func (c *Client) InstallBinary(ctx context.Context, name, channel string) (*BinaryInfo, error) {
	var out BinaryInfo
	if err := c.post(ctx, c.slow, "/api/binaries/install", InstallRequest{Name: name, Channel: channel}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Sites lists all served sites.
func (c *Client) Sites(ctx context.Context) ([]Site, error) {
	var out []Site
	if err := c.get(ctx, "/api/sites", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Settings returns the global settings map.
func (c *Client) Settings(ctx context.Context) (map[string]string, error) {
	var out map[string]string
	if err := c.get(ctx, "/api/settings", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Site mutations. Each returns the fresh site list after the daemon
// re-applied pools + Caddy config.

func (c *Client) Park(ctx context.Context, path string) ([]Site, error) {
	return c.siteMutation(ctx, "/api/sites/park", PathRequest{Path: path})
}

func (c *Client) Unpark(ctx context.Context, path string) ([]Site, error) {
	return c.siteMutation(ctx, "/api/sites/unpark", PathRequest{Path: path})
}

func (c *Client) Link(ctx context.Context, path, name string) ([]Site, error) {
	return c.siteMutation(ctx, "/api/sites/link", LinkRequest{Path: path, Name: name})
}

func (c *Client) Unlink(ctx context.Context, name string) ([]Site, error) {
	return c.siteMutation(ctx, "/api/sites/unlink", NameRequest{Name: name})
}

func (c *Client) Proxy(ctx context.Context, name, target string) ([]Site, error) {
	return c.siteMutation(ctx, "/api/sites/proxy", ProxyRequest{Name: name, Target: target})
}

// SetSitePHP pins a site's PHP version (isolate); version "" clears it.
func (c *Client) SetSitePHP(ctx context.Context, name, version string) ([]Site, error) {
	return c.siteMutation(ctx, "/api/sites/php", SitePHPRequest{Name: name, Version: version})
}

// SetSetting writes a global setting (e.g. php.default for `bench use`).
func (c *Client) SetSetting(ctx context.Context, key, value string) ([]Site, error) {
	return c.siteMutation(ctx, "/api/settings", SettingRequest{Key: key, Value: value})
}

func (c *Client) siteMutation(ctx context.Context, path string, body any) ([]Site, error) {
	var out []Site
	// Mutations can lazily start pools or even install Caddy on first
	// use, so use the untimed client and let ctx bound it.
	if err := c.post(ctx, c.slow, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Services lists all service instances.
func (c *Client) Services(ctx context.Context) ([]Service, error) {
	var out []Service
	if err := c.get(ctx, "/api/services", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ServiceCatalog lists the creatable service kinds for this platform.
func (c *Client) ServiceCatalog(ctx context.Context) ([]ServiceType, error) {
	var out []ServiceType
	if err := c.get(ctx, "/api/services/catalog", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateService installs, initializes and starts a new instance. Blocks
// through the download (minutes on first use); bound it with ctx.
func (c *Client) CreateService(ctx context.Context, req CreateServiceRequest) (*Service, error) {
	var out Service
	if err := c.post(ctx, c.slow, "/api/services", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// NewSite starts creating a Laravel app and returns its task at once;
// follow it with Events ("tasks").
func (c *Client) NewSite(ctx context.Context, req NewSiteRequest) (*Task, error) {
	var out Task
	if err := c.post(ctx, c.http, "/api/sites/new", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelTask asks a running task to stop.
func (c *Client) CancelTask(ctx context.Context, id string) error {
	return c.post(ctx, c.http, "/api/tasks/"+url.PathEscape(id)+"/cancel", struct{}{}, nil)
}

// ServiceAction runs start | stop on an instance.
func (c *Client) ServiceAction(ctx context.Context, name, action string) (*Service, error) {
	var out Service
	if err := c.post(ctx, c.slow, "/api/services/"+url.PathEscape(name)+"/"+action, struct{}{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CloneService copies an instance with its data into a new instance.
func (c *Client) CloneService(ctx context.Context, name, newName string) (*Service, error) {
	var out Service
	if err := c.post(ctx, c.slow, "/api/services/"+url.PathEscape(name)+"/clone", CloneServiceRequest{Name: newName}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetServiceAutostart toggles starting the instance with benchd.
func (c *Client) SetServiceAutostart(ctx context.Context, name string, enabled bool) (*Service, error) {
	var out Service
	if err := c.post(ctx, c.http, "/api/services/"+url.PathEscape(name)+"/autostart", AutostartRequest{Enabled: enabled}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UninstallBinary deletes an installed build (PHP only; the daemon refuses
// a version that the default or a site's pin still needs).
func (c *Client) UninstallBinary(ctx context.Context, name, channel string) error {
	path := "/api/binaries/" + url.PathEscape(name) + "/" + url.PathEscape(channel)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://"+c.Addr+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(c.slow, req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// DeleteService stops and removes an instance; keepData leaves its data
// directory on disk.
func (c *Client) DeleteService(ctx context.Context, name string, keepData bool) error {
	path := "/api/services/" + url.PathEscape(name)
	if keepData {
		path += "?keep_data=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://"+c.Addr+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(c.slow, req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Doctor runs the machine diagnostics.
func (c *Client) Doctor(ctx context.Context) ([]Check, error) {
	var out []Check
	if err := c.get(ctx, "/api/doctor", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Setup asks the daemon to run the elevated helper (DNS resolver rule +
// CA trust). Blocks through the user's elevation prompt.
func (c *Client) Setup(ctx context.Context, dns, trust bool) ([]string, error) {
	var out SetupResponse
	if err := c.post(ctx, c.slow, "/api/setup", SetupRequest{DNS: dns, Trust: trust}, &out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

// Teardown undoes Setup: removes the .test rule and/or the trusted CA.
func (c *Client) Teardown(ctx context.Context, dns, trust bool) ([]string, error) {
	var out SetupResponse
	if err := c.post(ctx, c.slow, "/api/setup/undo", SetupRequest{DNS: dns, Trust: trust}, &out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

// Events subscribes to the daemon's WS event stream. The channel closes
// when ctx ends or the connection drops.
func (c *Client) Events(ctx context.Context) (<-chan Event, error) {
	wsURL := "ws://" + c.Addr + "/api/events?token=" + url.QueryEscape(c.token)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("subscribing to daemon events: %w", err)
	}
	ch := make(chan Event)
	go func() {
		defer close(ch)
		defer conn.CloseNow()
		for {
			var ev Event
			if err := wsjson.Read(ctx, conn, &ev); err != nil {
				return
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.Addr+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(c.http, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return nil
}

func (c *Client) post(ctx context.Context, hc *http.Client, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.Addr+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(hc, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return nil
}

func (c *Client) do(hc *http.Client, req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := hc.Do(req)
	if err != nil {
		// A dial failure means nothing is listening there (or a stale addr
		// file after a crash). Anything else (e.g. a timeout from a hung
		// daemon) is a real error the caller must see, not "not running".
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return nil, ErrNotRunning
		}
		return nil, fmt.Errorf("calling daemon at %s: %w", c.Addr, err)
	}
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10)) // hints sit at the end of long errors
		resp.Body.Close()
		if m := strings.TrimSpace(string(msg)); m != "" {
			return nil, fmt.Errorf("daemon: %s (%s %s)", m, resp.Status, req.URL.Path)
		}
		return nil, fmt.Errorf("daemon returned %s for %s", resp.Status, req.URL.Path)
	}
	return resp, nil
}
