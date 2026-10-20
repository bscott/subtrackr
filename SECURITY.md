# Security Policy

## Reporting a vulnerability

Please do not report security vulnerabilities through public GitHub issues.

Email **security@byte-mail.xyz** with:

- a description of the issue and its impact,
- steps or a proof of concept to reproduce it,
- the SubTrackr version or commit you tested against, and
- whether authentication was enabled in your setup.

You should get an acknowledgement within a few days. Please allow time for a fix to be released before
disclosing the issue publicly; we will credit you in the release notes unless you prefer otherwise.

## Supported versions

Only the latest release on the `main` branch receives security fixes. If you run SubTrackr from the
Docker image, pull the latest tag to get them.

## Scope

In scope: the SubTrackr server (`cmd/server`, `internal/`), the MCP server (`cmd/mcp`), templates and
static assets. Reports about third-party dependencies are welcome when they include the SubTrackr code
path that makes them reachable.

Authentication is optional in SubTrackr. Issues that only exist when authentication is turned off are
deployment choices rather than vulnerabilities.
