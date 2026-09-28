package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// driver is everything service-specific: how to initialize a data dir,
// how to run and stop the server, and how a Laravel app connects to it.
// The Manager owns the lifecycle; drivers only describe.
type driver struct {
	label       string
	binary      string // binman manifest name
	defaultPort int
	extraPorts  []extraPort
	singleton   bool          // at most one instance (mailpit)
	stopTimeout time.Duration // clean-shutdown grace; 0 = supervisor default
	needsVCRT   bool          // Windows build links the MSVC runtime
	refusesRoot bool          // the server won't run as root (Unix)

	// prepare fixes up the installed build before init and every start
	// (e.g. linking a renamed system library); nil when nothing's needed.
	prepare func(in instance) error
	// init prepares an empty data dir; nil when the server creates its
	// own data on first start.
	init func(ctx context.Context, in instance) error
	// afterStart runs once the server is ready (e.g. create the S3 bucket
	// the .env block names); nil when nothing's needed.
	afterStart func(ctx context.Context, in instance) error
	// spec describes the server process (Name/LogFile are filled in by
	// the Manager).
	spec func(in instance) supervisor.Spec
	// env is the .env block that connects a Laravel app.
	env func(in instance) []string
	// afterClone scrubs identity files from a copied data dir.
	afterClone func(dataDir string) error
	// console is the web console's URL and the login it asks for; nil
	// when the service has none.
	console func(in instance) (string, []api.Credential)
}

// extraPort is a secondary listener (HTTP UI, admin console).
type extraPort struct {
	role        string
	defaultPort int
}

// drivers is the service catalog. Unknown kinds fail loudly at the API.
var drivers = map[string]driver{
	"mysql":       mysqlDriver("mysql"),
	"mariadb":     mysqlDriver("mariadb"),
	"postgresql":  postgresDriver(),
	"valkey":      valkeyDriver(),
	"meilisearch": meilisearchDriver(),
	"rustfs":      rustfsDriver(),
	"mailpit":     mailpitDriver(),
}

// driverOrder is the catalog display order.
var driverOrder = []string{"mysql", "mariadb", "postgresql", "valkey", "meilisearch", "rustfs", "mailpit"}

func loopback(port int) string { return "127.0.0.1:" + strconv.Itoa(port) }

// run executes a one-shot tool for an instance, logging to its log file.
// The tool's output becomes part of the error so failures are diagnosable
// from the API response alone.
func run(ctx context.Context, in instance, exe string, args ...string) error {
	out, err := supervisor.Exec(ctx, supervisor.Spec{Command: exe, Args: args, Dir: in.BinaryDir, LogFile: in.logFile()})
	if err != nil {
		return fmt.Errorf("%s failed: %w%s", filepath.Base(exe), err, withOutput(out))
	}
	return nil
}

func withOutput(out string) string {
	const max = 1500
	if out == "" {
		return ""
	}
	if len(out) > max {
		out = "…" + out[len(out)-max:]
	}
	return "\n" + out + hint(out)
}

// removeIfExists deletes a file that may legitimately be absent.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ---- MySQL / MariaDB ------------------------------------------------------

func mysqlDriver(flavor string) driver {
	server, admin, label := "mysqld", "mysqladmin", "MySQL"
	if flavor == "mariadb" {
		server, admin, label = "mariadbd", "mariadb-admin", "MariaDB"
	}
	return driver{
		label:       label,
		binary:      flavor,
		defaultPort: 3306,
		stopTimeout: 60 * time.Second, // InnoDB flushes dirty pages on clean shutdown
		needsVCRT:   flavor == "mysql",
		refusesRoot: true,
		prepare: func(in instance) error {
			if flavor == "mysql" {
				return linkLibaio(in.BinaryDir)
			}
			return nil
		},
		init: func(ctx context.Context, in instance) error {
			if flavor == "mariadb" {
				return mariadbInit(ctx, in)
			}
			// --initialize-insecure: root@localhost with an empty password,
			// matching a fresh Laravel .env. The trade-off: any local
			// account can log in as root over loopback, which on a
			// single-user dev machine is the user themself. The server never binds beyond 127.0.0.1.
			args := []string{"--no-defaults", "--initialize-insecure",
				"--basedir=" + in.BinaryDir, "--datadir=" + in.DataDir}
			return run(ctx, in, in.exe("bin", server), append(args, consoleArgs()...)...)
		},
		spec: func(in instance) supervisor.Spec {
			args := []string{"--no-defaults",
				"--basedir=" + in.BinaryDir, "--datadir=" + in.DataDir,
				"--port=" + strconv.Itoa(in.Port), "--bind-address=127.0.0.1",
				"--pid-file=" + filepath.Join(in.DataDir, server+".pid"),
				"--skip-log-bin", // a dev box doesn't replicate; binlogs just eat disk
			}
			args = append(args, mysqlPlatformArgs(in, flavor)...)
			return supervisor.Spec{
				Command: in.exe("bin", server),
				Args:    args,
				Dir:     in.BinaryDir,
				Health:  mysqlReady(loopback(in.Port)),
				// A failed clean shutdown (e.g. the user set a root password)
				// falls back to the OS stop; on Windows that's a hard kill and
				// InnoDB crash recovery runs on the next start. The supervisor
				// logs it.
				Shutdown: func(ctx context.Context) error {
					return run(ctx, in, in.exe("bin", admin), "--no-defaults", "--protocol=TCP",
						"-h127.0.0.1", "-P"+strconv.Itoa(in.Port), "-uroot", "shutdown")
				},
			}
		},
		env: func(in instance) []string {
			conn := "mysql"
			if flavor == "mariadb" {
				conn = "mariadb"
			}
			return []string{
				"DB_CONNECTION=" + conn, "DB_HOST=127.0.0.1", "DB_PORT=" + strconv.Itoa(in.Port),
				"DB_USERNAME=root", "DB_PASSWORD=",
			}
		},
		afterClone: func(dataDir string) error {
			// auto.cnf holds the server UUID; two servers sharing one breaks
			// replication tooling and confuses clients. It's regenerated.
			for _, f := range []string{"auto.cnf", server + ".pid"} {
				if err := removeIfExists(filepath.Join(dataDir, f)); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// mariadbInit creates the system tables with root@localhost on an empty
// password (a fresh Laravel .env's default). The Unix script defaults root to unix_socket
// auth, which would break TCP logins as root; Windows ships an .exe that
// takes different flags (and never --service, which needs admin).
func mariadbInit(ctx context.Context, in instance) error {
	if runtime.GOOS == "windows" {
		return run(ctx, in, in.exe("bin", "mariadb-install-db"), "--datadir="+in.DataDir, "--password=")
	}
	return run(ctx, in, filepath.Join(in.BinaryDir, "scripts", "mariadb-install-db"), "--no-defaults",
		"--basedir="+in.BinaryDir, "--datadir="+in.DataDir,
		"--auth-root-authentication-method=normal", "--skip-test-db")
}

// ---- PostgreSQL -------------------------------------------------------------

func postgresDriver() driver {
	return driver{
		label:       "PostgreSQL",
		binary:      "postgresql",
		defaultPort: 5432,
		stopTimeout: 60 * time.Second,
		needsVCRT:   true,
		refusesRoot: true,
		init: func(ctx context.Context, in instance) error {
			// Superuser "root" with trust auth, like a fresh Laravel .env (DB_USERNAME=root,
			// empty password). The trade-off: any local account can connect
			// as a superuser over loopback (and so run programs as the Bench
			// user via COPY ... PROGRAM); on a single-user dev machine that
			// is the user themself. Loopback-only, see spec.
			return run(ctx, in, in.exe("bin", "initdb"), "-D", in.DataDir, "-U", "root",
				"--auth=trust", "-E", "UTF8", "--locale=C", "--no-instructions")
		},
		spec: func(in instance) supervisor.Spec {
			return supervisor.Spec{
				Command: in.exe("bin", "postgres"),
				Args: []string{"-D", in.DataDir, "-p", strconv.Itoa(in.Port),
					"-c", "listen_addresses=127.0.0.1",
					"-c", "unix_socket_directories=", // TCP only: no /tmp sockets to collide
				},
				Dir:    in.BinaryDir,
				Health: postgresReady(loopback(in.Port)),
				Shutdown: func(ctx context.Context) error {
					// pg_ctl signals via postmaster.pid on every OS (it emulates
					// signals on Windows), so it works on a postgres it didn't start.
					return run(ctx, in, in.exe("bin", "pg_ctl"), "stop", "-D", in.DataDir, "-m", "fast", "-w", "-t", "55")
				},
			}
		},
		env: func(in instance) []string {
			return []string{
				"DB_CONNECTION=pgsql", "DB_HOST=127.0.0.1", "DB_PORT=" + strconv.Itoa(in.Port),
				"DB_USERNAME=root", "DB_PASSWORD=",
			}
		},
		afterClone: func(dataDir string) error {
			if err := removeIfExists(filepath.Join(dataDir, "postmaster.pid")); err != nil {
				return err
			}
			// Postgres refuses a data dir readable by group/others.
			return os.Chmod(dataDir, 0o700)
		},
	}
}

// ---- Valkey (Redis-compatible) ------------------------------------------------

func valkeyDriver() driver {
	return driver{
		label:       "Valkey (Redis)",
		binary:      "valkey",
		defaultPort: 6379,
		stopTimeout: 30 * time.Second, // SHUTDOWN saves the dataset first
		spec: func(in instance) supervisor.Spec {
			return supervisor.Spec{
				Command: in.valkeyServer(),
				Args: []string{"--port", strconv.Itoa(in.Port), "--bind", "127.0.0.1",
					"--protected-mode", "yes", "--daemonize", "no",
					// Forward slashes: the Windows (msys2) build mis-parses backslashes.
					"--dir", filepath.ToSlash(in.DataDir),
					// Valkey writes the log itself: the msys2 build block-buffers
					// stdout into a Windows file handle and the lines never land.
					"--logfile", filepath.ToSlash(in.logFile())},
				Dir:      in.DataDir,
				Health:   redisReady(loopback(in.Port)),
				Shutdown: redisShutdown(loopback(in.Port)),
			}
		},
		env: func(in instance) []string {
			// predis: Bench's PHP builds don't ship the phpredis extension
			// (the app needs `composer require predis/predis`).
			return []string{"REDIS_CLIENT=predis", "REDIS_HOST=127.0.0.1", "REDIS_PORT=" + strconv.Itoa(in.Port), "REDIS_PASSWORD=null"}
		},
	}
}

// ---- Meilisearch --------------------------------------------------------------

func meilisearchDriver() driver {
	return driver{
		label:       "Meilisearch",
		binary:      "meilisearch",
		defaultPort: 7700,
		spec: func(in instance) supervisor.Spec {
			// A per-instance master key: Meilisearch answers every origin
			// with CORS *, so without one any website open in the browser
			// could read and delete local indexes. (/health stays public.)
			// Passed in the environment, not argv, so ps doesn't show it.
			return supervisor.Spec{
				Command: in.exe("", "meilisearch"),
				Args: []string{"--http-addr", loopback(in.Port), "--env", "development", "--no-analytics",
					"--db-path", filepath.Join(in.DataDir, "data.ms"),
					"--dump-dir", filepath.Join(in.DataDir, "dumps"),
					"--snapshot-dir", filepath.Join(in.DataDir, "snapshots")},
				Env:    []string{"MEILI_MASTER_KEY=" + in.cfg.MasterKey},
				Dir:    in.DataDir,
				Health: httpReady("http://" + loopback(in.Port) + "/health"),
			}
		},
		env: func(in instance) []string {
			return []string{"SCOUT_DRIVER=meilisearch", "MEILISEARCH_HOST=http://" + loopback(in.Port), "MEILISEARCH_KEY=" + in.cfg.MasterKey}
		},
		// --env development serves the search preview dashboard at /.
		console: func(in instance) (string, []api.Credential) {
			return "http://" + loopback(in.Port), []api.Credential{{Label: "API key", Value: in.cfg.MasterKey, Secret: true}}
		},
	}
}

// ---- RustFS (S3-compatible; MinIO stopped shipping binaries in 2025) ------

// rustfsBucket is created on start so AWS_BUCKET works as-is. Credentials
// are per instance (config.AccessKey/SecretKey): fixed ones would be
// public, and SigV4 happily signs requests from a DNS-rebound page.
const rustfsBucket = "local"

func rustfsDriver() driver {
	return driver{
		label:       "RustFS (S3)",
		binary:      "rustfs",
		defaultPort: 9000,
		extraPorts:  []extraPort{{role: "console", defaultPort: 9001}},
		spec: func(in instance) supervisor.Spec {
			return supervisor.Spec{
				Command: in.exe("", "rustfs"),
				Args: []string{"--address", loopback(in.Port),
					"--console-enable", "--console-address", loopback(in.cfg.Ports["console"]),
					in.DataDir},
				// RustFS logs nothing to stdout unless asked to, is very chatty
				// at info, and phones home for updates (Bench owns updates).
				Env: []string{"RUSTFS_OBS_LOG_STDOUT_ENABLED=true", "RUSTFS_OBS_LOGGER_LEVEL=warn",
					"RUSTFS_CHECK_UPDATE=false",
					"RUSTFS_ACCESS_KEY=" + in.cfg.AccessKey, "RUSTFS_SECRET_KEY=" + in.cfg.SecretKey},
				Dir: in.DataDir,
				// Not a TCP dial: the port answers before the storage layer
				// has quorum, and requests then fail with 503.
				Health: httpReady("http://" + loopback(in.Port) + "/health/ready"),
			}
		},
		afterStart: func(ctx context.Context, in instance) error {
			return ensureBucket(ctx, "http://"+loopback(in.Port), in.cfg.AccessKey, in.cfg.SecretKey, rustfsBucket)
		},
		env: func(in instance) []string {
			return []string{
				"FILESYSTEM_DISK=s3", "AWS_ACCESS_KEY_ID=" + in.cfg.AccessKey, "AWS_SECRET_ACCESS_KEY=" + in.cfg.SecretKey,
				"AWS_DEFAULT_REGION=us-east-1", "AWS_BUCKET=" + rustfsBucket, "AWS_ENDPOINT=http://" + loopback(in.Port),
				"AWS_USE_PATH_STYLE_ENDPOINT=true",
			}
		},
		console: func(in instance) (string, []api.Credential) {
			return "http://" + loopback(in.cfg.Ports["console"]), []api.Credential{
				{Label: "Access key", Value: in.cfg.AccessKey},
				{Label: "Secret key", Value: in.cfg.SecretKey, Secret: true},
			}
		},
	}
}

// ---- Mailpit ----------------------------------------------------------------

// mailpitUser is the basic-auth user for Mailpit's web UI and API; the
// password is per instance. Without it, a DNS-rebound web page could read
// every caught mail (password-reset links included). SMTP stays open:
// apps log in with any username (their APP_NAME) to tag their inbox.
const mailpitUser = "bench"

func mailpitDriver() driver {
	return driver{
		label:       "Mail (Mailpit)",
		binary:      "mailpit",
		defaultPort: 2525, // the MAIL_PORT a fresh Laravel .env expects
		extraPorts:  []extraPort{{role: "http", defaultPort: 8025}},
		singleton:   true,
		spec: func(in instance) supervisor.Spec {
			http := loopback(in.cfg.Ports["http"])
			return supervisor.Spec{
				Command: in.exe("", "mailpit"),
				Args: []string{"--smtp", loopback(in.Port), "--listen", http,
					"--database", filepath.Join(in.DataDir, "mailpit.db"),
					// Any SMTP login is accepted and becomes a tag, so
					// MAIL_USERNAME="${APP_NAME}" yields per-project inboxes.
					"--smtp-auth-accept-any", "--smtp-auth-allow-insecure", "--tags-username"},
				Env:    []string{"MP_UI_AUTH=" + mailpitUser + ":" + in.cfg.UIPassword},
				Dir:    in.DataDir,
				Health: httpReady("http://" + http + "/livez"),
			}
		},
		env: func(in instance) []string {
			return []string{
				"MAIL_MAILER=smtp", "MAIL_HOST=127.0.0.1", "MAIL_PORT=" + strconv.Itoa(in.Port),
				`MAIL_USERNAME="${APP_NAME}"`, "MAIL_PASSWORD=null",
			}
		},
	}
}
