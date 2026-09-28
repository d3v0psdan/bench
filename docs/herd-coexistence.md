# Coexisting with Laravel Herd

Bench and Laravel Herd can be installed on the same machine. They can't
both serve sites at the same time, because both want the HTTPS port.

## The short version

- Installing Bench doesn't touch Herd's files, services or settings, and
  installing Herd doesn't touch Bench's.
- Only one of them can serve sites at a time, since both serve
  `https://*.test` on port 443. Quit Herd (tray icon, then Quit Herd)
  before using Bench, or run `bench stop` before starting Herd.
- Services and mail can run side by side. When Herd already holds a port
  (3306, 6379, 7700, 2525 ...), Bench starts the instance on the next free
  port and shows the port to put in your `.env`.

## Finding out what's in the way

Run `bench doctor`. It names the program holding each port Bench needs
and says what to do, for example:

```
[fail] HTTPS port 443: Laravel Herd's nginx.exe (pid 17404) holds port 443, so Bench can't serve https://*.test.
       fix: Bench and Herd can be installed side by side, but only one can serve sites at a time: ...
[warn] Mail (SMTP): Mail catcher is running on 127.0.0.1:2526 instead of 2525 (the default was taken).
       fix: Set MAIL_PORT=2526 in your apps' .env.
```

`bench start` runs the same checks and fails when the HTTPS port is
taken. Bench recognises Herd by its install locations (`Program Files\Herd`
and `~\.config\herd` on Windows, `/Applications/Herd.app` and
`~/Library/Application Support/Herd` on macOS), so your own projects in
`~/Herd` never count as Herd.

## Details

| Resource | Herd | Bench | Conflict? |
|---|---|---|---|
| HTTPS `*.test` | nginx on 127.0.0.1:443 | Caddy on 127.0.0.1:443 | Yes, one at a time |
| HTTP `*.test` | nginx on 127.0.0.1:80 | not used (HTTPS only) | Plain `http://` links reach Herd |
| `.test` DNS | hosts-file entries (Windows), dnsmasq (macOS) | DNS stub plus NRPT, resolver file or systemd-resolved | No: both answer 127.0.0.1 |
| Mail | :2525 on all interfaces, when Herd's mail service runs | Mailpit on 127.0.0.1:2525, or the next free port | Bench moves aside |
| PHP | php-cgi on 90XX/91XX | php-cgi on 236XX | No |
| Services | 3306, 5432, 6379, 7700, 9000 | same defaults, next free port when taken | Bench moves aside |
| Helper | HerdHelper on :5000 (through Windows HTTP.sys) | none | No |

When Bench picks ports automatically it skips :5000 and 9002-9199 (Herd's
helper and PHP pools), so starting Herd later doesn't collide with a
Bench service.

## Switching from Herd

Herd's `.env` values mostly carry over: `DB_USERNAME=root` with an empty
password, `REDIS_HOST=127.0.0.1` and `MAIL_USERNAME="${APP_NAME}"` all work
unchanged. Update the ports if Bench had to move them (`bench services:env
<name>` prints the exact lines), and replace `MEILISEARCH_KEY` with the key
Bench shows for your Meilisearch instance.
