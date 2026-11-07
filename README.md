# Latere Status

The source of [status.latere.ai](https://status.latere.ai): the current state and 90-day history of Latere's public services.

It runs on GitHub, not on Latere's infrastructure, so it keeps working when Latere is down. A scheduled workflow probes the public endpoints from GitHub's runners every five minutes, records the results, and publishes a static page to GitHub Pages.

## What it checks

Each service is one HTTPS GET with a 10 second timeout and no redirects followed. A check passes when the status code matches and, if set, the body contains the expected text. The list is in [`internal/status/services.go`](internal/status/services.go).

| Service | URL |
| --- | --- |
| Website | `https://latere.ai/` |
| Latere Chat | `https://chat.latere.ai/readyz` |
| Platform console | `https://platform.latere.ai/healthz` |
| API | `https://api.latere.ai/readyz` |
| Sign-in | `https://auth.latere.ai/.well-known/openid-configuration` |
| Repos | `https://code.latere.ai/readyz` |
| Apps | `https://latere.site/` |

File delivery (`cdn.latere.ai`) is not checked: it has no public object that is guaranteed to stay.

Two checks in a row must fail before a service shows as down. A check that passes but takes longer than 3 seconds is slow, and two slow checks in a row show a service as degraded. A single bad check is a blip and is not counted. A day's bar is red when uptime was under 99%, amber when it had any confirmed down or slow checks, and green otherwise.

## Add a service

Add one line to `Services` in `internal/status/services.go`: an `ID` (stable, never renamed, it keys the history), a `Name` for the page, the `URL`, the `Expect`ed status code, and an optional `Contains` body check. Use only endpoints that answer without credentials. Run `go test ./...`.

## Write an incident

Add a file to `incidents/`, for example `incidents/2026-11-04-chat-errors.md`, and push it to `main`. Copy `incidents/_template.md`. Files that start with `_` and `README.md` are ignored.

```markdown
---
title: Latere Chat returns errors
services: [chat]
started: 2026-11-04T10:00:00Z
resolved:
updates:
  - time: 2026-11-04T10:10:00Z
    status: Investigating
    text: We are looking into it.
---
Some conversations fail to load.
```

`services` are the IDs from `services.go`. Leave `resolved` empty while the incident is open and set it when it ends. Open incidents show at the top of the page. Resolved ones show for 90 days. Times are UTC. A file with a mistake fails the build with its name, and the page keeps showing the last good version until it is fixed.

## How it works

`.github/workflows/status.yml` runs on a five minute schedule (GitHub may delay it), on manual dispatch, and on a push to `main` that changes incidents or code.

1. `update` job: checks out the `history` branch beside `main`, runs `status probe` through `scripts/commit-history.sh`, then runs `status build` and uploads `site/` as the Pages artifact. It has `contents: write` only.
2. `deploy` job: publishes the artifact with `actions/deploy-pages`. It has `pages: write` and `id-token: write` only, and no `environment:`, so a publish every few minutes does not record a GitHub deployment or notify deployment subscribers.

History is one file per UTC day, `YYYY-MM-DD.json`, on the `history` branch, so `main` stays clean. `probe` drops files older than 90 days. If a push is rejected because another run pushed first, `commit-history.sh` rebases and tries again. If the rebase conflicts, it takes the remote branch as it is and probes again. It never force-pushes. The branch's git history still holds old days, so it grows slowly; squash it by hand if it ever matters.

`status build` renders `index.html` from history and incidents with `html/template`, and copies `style.css` and the fonts (Inter and Instrument Serif, both under the SIL Open Font License) from `internal/status/web/`. The page loads nothing from other hosts.

### Set up once

In the repository settings, set Pages to deploy from GitHub Actions and the custom domain to `status.latere.ai`. `site/CNAME` carries the domain. Point a `CNAME` record for `status` at `latere-ai.github.io`.

## Develop

```sh
go test ./...
go run ./cmd/status build -history testdata/history -incidents testdata/incidents -out /tmp/site -now 2026-11-04T12:00:00Z
scripts/commit-history_test.sh
```

After a deliberate change to the page, run `go test ./internal/status -update` and review the diff of `testdata/index.golden.html`.

## License

Apache-2.0. Copyright 2026 Latere AI.
