# Time-Automation

A Go scheduler with a Playwright/Chromium authentication helper, packaged in a Docker image, for automating time-tracking workflows through API calls.

It randomly schedules work and break start/stop actions within configurable time windows, skips holidays and vacation days via ICS calendar integration, persists state across restarts, sends Discord webhook notifications, and exposes a web status UI.

## Features

- Randomised work and break start times within configurable windows
- Enforces minimum and maximum durations for work and break
- ICS calendar integration to skip public holidays and vacation days
- Persists daily state to JSON — restarts never re-trigger already-completed actions
- Authentik browser login, persisted sessions, CSRF protection and idempotent booking retries
- Discord webhook notifications for planned times, errors, and day-off events
- Web status UI at `http://<host>:8077` — shows today's status, timeline, and configuration
- Graceful shutdown on `SIGTERM` / `SIGINT`; panic recovery keeps the service alive
- Dry-run mode for testing without making actual API calls
- Go standard library scheduler plus Node.js, Playwright and Chromium

## Deployment

### Docker Compose / Portainer

The included `docker-compose.yml` is ready to use as a Portainer stack.  
Set the required environment variables in Portainer's stack UI before deploying.

```bash
docker compose up -d
```

State is persisted in the named volume `time-automation-state` mounted at `/data`.  
The status UI is available on port `8077` (configurable via `WEB_PORT`).

### Docker CLI

```bash
docker run -d \
  --name time-automation \
  --restart unless-stopped \
  --env-file .env \
  -v time-automation-state:/data \
  -p 8077:8077 \
  ghcr.io/audioinj/time-automation:latest
```

### Build from Source

```bash
go build -o time-automation .
./time-automation
```

Or with Docker:

```bash
docker build -t time-automation .
docker run --env-file .env -v time-automation-state:/data -p 8077:8077 time-automation
```

## Configuration

Copy `.env.example` to `.env` and fill in your values.

### Required

| Variable | Description |
|---|---|
| `DOMAIN` | Base domain of the time-tracking service (e.g. `service.com`) |
| `SUBDOMAIN` | Subdomain prefix (e.g. `time` → `time.service.com`) |
| `USERNAME` | Login username |
| `PASSWORD` | Login password |
| `START_WORK_MIN` | Earliest work start time (`HH:MM`) |
| `START_WORK_MAX` | Latest work start time (`HH:MM`) |
| `START_BREAK_MIN` | Earliest break start time (`HH:MM`) |
| `START_BREAK_MAX` | Latest break start time (`HH:MM`) |
| `MIN_WORK_DURATION` | Minimum total work duration (e.g. `8.5h`) |
| `MAX_WORK_DURATION` | Maximum total work duration (e.g. `9.5h`) |
| `MIN_BREAK_DURATION` | Minimum break duration (e.g. `0.75h`) |
| `MAX_BREAK_DURATION` | Maximum break duration (e.g. `1.25h`) |

Constraint: `MAX >= MIN` for all paired values — the service exits with an error otherwise.

### Optional

| Variable | Default | Description |
|---|---|---|
| `WORK_DAYS` | `1-5` | Weekday range or list (0=Sun … 6=Sat), e.g. `1-5` or `1,3,5` |
| `TASK` | — | Task label sent with each time entry |
| `WEBHOOK_URL` | — | Discord webhook URL for notifications |
| `HOLIDAY_ADDRESS` | — | Public ICS URL for public holidays |
| `VACATION_ADDRESS` | — | ICS URL for vacation calendar (fetched with HTTP Basic Auth) |
| `VACATION_KEYWORD` | — | Filter vacation events by keyword (case-insensitive) |
| `STATE_FILE` | `/app/state.json` | Path to the JSON state file |
| `ICS_CACHE_DIR` | directory of `STATE_FILE` | Directory where ICS files are cached |
| `WEB_PORT` | `8077` | Port for the status web UI |
| `VERBOSE` | `false` | Enable verbose logging |
| `DRY_RUN` | `false` | Skip actual API calls (safe for testing) |

## Status UI

The service exposes a read-only HTTP interface on `WEB_PORT` (default `8077`):

| Route | Description |
|---|---|
| `GET /` | HTML status page, auto-refreshes every 30 s |
| `GET /api/status` | Same data as JSON |

The page shows the current day's work and break status, net working time, the day's timeline with planned and actual times, and the active configuration. No passwords or secrets are exposed.

## How It Works

The scheduler ticks every 5 seconds and evaluates the current time against four randomised daily thresholds:

```
START_WORK → START_BREAK → STOP_BREAK → STOP_WORK
```

1. **Daily planning** — On the first tick of a new day, all four times are randomised within the configured windows and saved to state. Subsequent restarts load the same planned times, so the schedule survives container restarts.
2. **Holiday/vacation check** — ICS calendars are fetched once per day and cached in `ICS_CACHE_DIR`. If today matches a holiday or vacation event the scheduler skips all actions and sends a notification.
3. **Action gating** — Each action fires exactly once; the persisted state prevents re-triggering on restart.
4. **Duration guards** — Work stop is held back until `MIN_WORK_DURATION` has elapsed. Break stop is held back until `MIN_BREAK_DURATION` has elapsed.
5. **Session renewal** — Expired web sessions trigger a new Authentik browser login. Every write uses the session CSRF token and a stable per-user/day/action idempotency key. Failed actions remain pending, with a one-minute retry cooldown.
6. **Context propagation** — The shutdown context is passed to all HTTP requests, so in-flight calls are cancelled immediately on `SIGTERM`.

## CI / CD

| Workflow | Trigger | What it does |
|---|---|---|
| `ci.yml` | Push (non-main) / PR | Tests with race detector, coverage report, golangci-lint, govulncheck, Docker build check on PRs |
| `docker-build.yml` | Push to `main` | Builds and pushes `:latest` + `:sha-<commit>` to `ghcr.io/audioinj/time-automation` |
| `release.yml` | Push of `v*.*.*` tag | Builds and pushes semver-tagged image (`1.2.3`, `1.2`, `1`), creates GitHub Release with auto-generated notes |
| `renovate.yml` | Monday 05:00 UTC / config push / manual | Updates Go, Docker images and GitHub Actions; supports dry runs |

Build and release workflows use the built-in `GITHUB_TOKEN`.

### Dependency updates

Renovate replaces Dependabot version updates and follows the Paperless/calibre policy:

- Weekly updates before 08:00 on Monday, Europe/Berlin.
- Minor and patch updates are grouped and automatically merged after CI passes.
- Major updates require approval in the Dependency Dashboard before a PR is created, and must then be merged manually.
- Security alerts can be processed at any time when Renovate runs.

Add an Actions repository secret named `RENOVATE_TOKEN` with a fine-grained PAT that can access this repository. It needs **Contents**, **Pull requests**, **Issues** and **Workflows** read/write permissions, plus **Dependabot alerts** read permission for security updates. A token stored in another repository is not automatically available here.

Renovate performs merges itself after successful checks (`platformAutomerge: false`), so GitHub's **Allow auto-merge** setting is not required. A later Renovate run merges eligible PRs once CI has finished; the workflow can also be started manually.

The workflow fails with a clear message if the secret is missing. It deliberately requires the PAT because PRs created with `GITHUB_TOKEN` do not trigger CI. To verify the setup, run **Actions → Renovate → Run workflow** with **dryRun** enabled; run again without dry-run to process updates within the configured schedule.

Other PRs are automatically squash-merged once all CI checks pass. Renovate PRs use Renovate's update-specific automerge policy.

## Development

```bash
go test -race ./...   # run tests with race detector
go vet ./...          # static analysis
go build ./...        # compile
```

Test coverage includes: scheduler state machine (`Run`), ICS parsing, state persistence and corruption handling, executor error propagation and idempotency keys, Discord webhook payload, and config validation.

Linting is configured in `.golangci.yml` (bodyclose, errcheck, staticcheck, noctx, and more). Run locally with:

```bash
golangci-lint run
```


## Authentik browser authentication

The updated time service uses OIDC web sessions, not the former `/api/login` token.
Set `DOMAIN=in-factory.com`, `SUBDOMAIN=time`, `AUTH_ORIGIN=https://auth.in-factory.com`,
and the existing `USERNAME`/`PASSWORD` in your deployment secrets/environment.
`AUTH_ORIGIN` must be the exact HTTPS origin without a trailing slash; when unset
it defaults to `https://auth.${DOMAIN}`. The configured username must match the
service's normalized `preferred_username`. No MFA or interactive consent is supported.

The helper handles Authentik username/password forms (including separate stages and
open shadow roots), stores cookies beside `STATE_FILE` as `browser-session.json`,
and reads `/api/session` for identity and CSRF before booking. Credentials travel
through subprocess stdin, never command arguments. Session files are written
atomically with mode 0600; protect the persistent volume and its backups as secrets.
Only the configured service and Authentik origins are allowed for browser requests.
Other identity providers, CAPTCHA, external assets or customized login forms may
require adapting the helper. Login failures are bounded and never logged with secrets.

Build and deploy a new image; an existing `latest` image will not contain these changes.
The Compose volume persists the browser session across restarts. To verify login
without making a time booking, run in the newly built container:

```sh
docker compose run --rm --no-deps time-automation ./time-automation -check-auth
```

Stop the running scheduler before this check so two processes do not share a session
file. `-check-auth` intentionally performs authentication even with `DRY_RUN=true`.
Normal dry runs make no browser or API calls.

Bookings keep the existing status values and task text; the server determines the
user from the session. The helper sends `X-CSRF-Token` and `Idempotency-Key`, rejects
redirects/HTML/unknown success bodies, and renews expired sessions. Retries reuse the
same key across process restarts and manual/scheduled actions for the same local day.
Only one instance may use a state volume. Resetting a day does not create new operation
keys; it must not be used to book an additional work/break cycle on that day. The server
retains receipts for 30 days. Avoid changing TASK during an unresolved booking because
the server rejects different payloads under the same key.

For a local build, install Node.js and the locked browser dependency in addition to Go:

```sh
npm --prefix browser ci
cd browser && npx playwright install --with-deps chromium && cd ..
go build -o time-automation .
npm --prefix browser test
```

Run from the repository root, or set `BROWSER_HELPER` to the absolute `client.mjs` path.
`BROWSER_SESSION_FILE` overrides the session location; `CHROMIUM_PATH` selects an
installed Chromium (used by Docker). Browser tests use a disposable local HTTPS
fixture with synthetic credentials, not the production identity provider.
