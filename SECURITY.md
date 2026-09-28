# Security policy

Bench has a larger attack surface than most dev tools: a localhost daemon with a token-authenticated API, a local DNS resolver for `*.test`, a locally trusted certificate authority, and a small elevated helper (UAC/sudo/polkit). Reports against any of these are treated as high priority.

## Reporting a vulnerability

Please don't open a public issue. Use GitHub's private vulnerability reporting: on this repository's Security tab, click "Report a vulnerability". Include reproduction steps and the OS you tested on.

You'll get an acknowledgment within 72 hours. Reporters are credited in the release notes unless they ask otherwise.

## Scope of particular interest

- Bypassing the daemon API token or reaching the API from a non-localhost origin
- DNS rebinding against the `*.test` resolver or the API
- Privilege escalation via `bench-helper`
- Abuse of the local CA / trust-store installation
- Tampering with binary downloads (manifest/checksum verification)

## Supported versions

Pre-1.0: only the latest release receives security fixes.

## Contact

Email: `d3v0psdan@gmail.com`
