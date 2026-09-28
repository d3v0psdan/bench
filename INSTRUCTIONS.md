# Using Bench

Bench serves your Laravel and PHP apps at `https://<name>.test` and runs their
databases, caches and mail locally, with no Docker. You can do everything from
the desktop app or from the `bench` command line; both talk to the same
background service (`benchd`), so sites keep running when the app is closed.

Building from source instead? See [README.md](README.md). What's planned next
is in [ROADMAP.md](ROADMAP.md).

## 1. First run

1. Open Bench. It starts its background service for you; the status pill in
   the header turns green.
2. Follow **Getting started** on the Overview page:
   - **Install PHP.** Pick a version on the Runtimes page (8.4 is a good default).
   - **Run setup.** One admin prompt lets Bench route `*.test` to your machine
     and trust its local HTTPS certificates, so browsers show no warnings.
3. Add a site (next section).

Already using Laravel Herd? Both can stay installed. Bench moves its services
to free ports and tells you which; see [docs/herd-coexistence.md](docs/herd-coexistence.md).

## 2. Add a site

Click **Add site** on the Sites or Overview page, then choose one:

| Choice | What it does |
|---|---|
| **New Laravel app** | Asks the same questions as `laravel new` (starter kit, auth, tests, Boost, database, npm or bun), creates the app in your projects folder (default `~/Bench`) and serves it. Progress shows in Activity; you can keep working or cancel. |
| **Link a project** | Serves one existing folder as `https://<name>.test`. |
| **Park a folder** | Serves every project inside a folder, each under its folder name. New projects appear on their own. |
| **Proxy a local port** | Serves `https://<name>.test` from a port, for Node, Go or any other server. |

Laravel apps are served from their `public/` folder.

## 3. Work with a site

Open a site from the Sites list, the site switcher in the header, or the
command palette. On its page you can:

- Click the name to open it in your browser, or the path to show the folder.
- **Open in editor**, and in the **...** menu **Open terminal here** and **Open database**.
- **Update .env** (in the **...** menu): Bench writes the database, cache and
  mail settings into the app's `.env`, marked `# Bench generated`. For an
  existing app you see exactly what changes before anything is written.
- In **Settings**: pin a PHP version for this site, or change a proxy's port.
- **Delete site** (in the **...** menu or Settings): stops serving it, and can
  also move its folder to the Recycle Bin (Trash on macOS and Linux), drop its
  database, delete an instance Bench made just for it, and clear its mail and
  creation log. Each part has its own switch. A parked site is served from its
  folder, so deleting it always moves the folder. Anything another site also
  uses (a shared instance, a database or inbox another app points at) is kept.

## 4. Services: databases, caches, search, storage

On the **Services** page click **New service** and pick MySQL, MariaDB,
PostgreSQL, Valkey (Redis), Meilisearch or RustFS (S3). Bench downloads it,
starts it and shows the `.env` lines to use (or use **Update .env** on a site).

- Click a service to see its details, **Open in client** or **Open console**,
  and its **Logs**. A failed service always offers **View logs**.
- **Start with Bench** starts it whenever Bench starts.
- Clone a service with its data, or delete it, from its menu.
- Everything listens on 127.0.0.1 only. Databases use `root` with no password.
- For Valkey, run `composer require predis/predis` in your app (the `.env`
  lines set `REDIS_CLIENT=predis`).

## 5. Mail

Open **Mail** and turn it on. Apps send to `127.0.0.1` on the port shown
(2525, or the next free one). Set `MAIL_USERNAME="${APP_NAME}"` and each app
gets its own inbox. You can search, read, preview HTML safely, save
attachments, and delete messages.

## 6. Runtimes

The **Runtimes** page installs and removes PHP versions and sets the global
default. Click a version to see where it lives, its `php.ini` and its
extensions.

## 7. Everyday tips

- **Command palette:** Ctrl+K (Cmd+K on macOS) finds any site, service or action.
- **Pages:** Ctrl+1 Overview, 2 Sites, 3 Services, 4 Runtimes, 5 Mail,
  6 Activity, Ctrl+, Settings (Cmd on macOS). Ctrl+B collapses the sidebar.
- **Activity** (the tray at the bottom, or its page) shows every download,
  install and new app, with progress, cancel and logs.
- **Tray icon:** open sites, see status, or stop Bench without opening the window.
- **Settings:** theme, your editor, start at sign-in, notifications, HTTPS and
  DNS, the update check (About), and **Diagnostics** when something's wrong.
- **Projects folder:** new apps go into `~/Bench`, which Bench parks. To put
  one app somewhere else, click **Change…** next to Folder when you name it.

## 8. Command line

Everything in the app is also a `bench` command. The common ones:

```sh
bench status                      # is Bench running?
bench php:install 8.4             # install a PHP version
bench use 8.4                     # set the global default
bench setup                       # *.test DNS + trusted HTTPS (one admin prompt)

bench new shop --react --database=mysql   # new Laravel app at https://shop.test
cd ~/code/my-app && bench link    # serve this folder as https://my-app.test
bench park ~/Sites                # serve every project in ~/Sites
bench proxy api 3000              # https://api.test -> localhost:3000
bench isolate 8.3                 # pin this site to PHP 8.3 (unisolate to undo)
bench sites                       # list sites

bench services:create mysql 8.4 shop-db   # new database; prints its .env lines
bench services:list               # instances, ports and state
bench services:env shop-db        # the .env lines again
bench db shop-db                  # open it in TablePlus, DBeaver or a terminal

bench doctor                      # port conflicts, DNS and setup checks
bench setup --undo                # remove the DNS rule and the trusted CA
bench stop                        # stop Bench and everything it runs
```

Run `bench help <command>` for every flag.

## 9. When something goes wrong

1. Open **Settings > Diagnostics** (or run `bench doctor`). Each problem comes
   with its fix, and you can copy a report.
2. For a failed service, click **View logs**. For a new app that failed,
   **Show log file** in its notification or in Activity.
3. If Bench won't start, the start screen offers **Open logs folder** and
   **Try again**.
4. Still stuck? Use **Report an issue** in Settings > About and attach the report.

## Uninstalling

Uninstalling Bench removes its `*.test` DNS rule and its trusted certificate
authority. Your projects are never touched. Bench's own data (PHP builds,
databases, mail) stays in `~/.bench`; delete that folder to remove it.
