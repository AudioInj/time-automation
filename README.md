# Time-Automation

A lightweight Go application, packaged in an Alpine-based Docker image, for automating time-tracking workflows through API calls.

It randomly schedules work and break start/stop actions within configurable time windows, skips holidays and vacation days via ICS calendar integration, persists state across restarts, sends Discord webhook notifications, and exposes a web status UI.

## Features

- Randomised work and break start times within configurable windows
- Enforces minimum and maximum durations for work and break
- ICS calendar integration to skip public holidays and vacation days
- Persists daily state to JSON — restarts never re-trigger already-completed actions
- Retries API calls up to 5 times; automatically re-authenticates on expired tokens (401)
- Discord webhook notifications for planned times, errors, and day-off events
- Web status UI at `http://<host>:8077` — shows today's status, timeline, and configuration
- Graceful shutdown on `SIGTERM` / `SIGINT`; panic recovery keeps the service alive
- Dry-run mode for testing without making actual API calls
- Zero external dependencies — pure Go standard library

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
5. **Token refresh** — If the API returns 401, the cached token is discarded and a fresh login is attempted before retrying.
6. **Context propagation** — The shutdown context is passed to all HTTP requests, so in-flight calls are cancelled immediately on `SIGTERM`.

## CI / CD

| Workflow | Trigger | What it does |
|---|---|---|
| `ci.yml` | Push (non-main) / PR | Tests with race detector, coverage report, golangci-lint, govulncheck, Docker build check on PRs |
| `docker-build.yml` | Push to `main` | Builds and pushes `:latest` + `:sha-<commit>` to `ghcr.io/audioinj/time-automation` |
| `release.yml` | Push of `v*.*.*` tag | Builds and pushes semver-tagged image (`1.2.3`, `1.2`, `1`), creates GitHub Release with auto-generated notes |

No additional secrets required — all workflows use the built-in `GITHUB_TOKEN`.

PRs are automatically squash-merged once all CI checks pass.

## Development

```bash
go test -race ./...   # run tests with race detector
go vet ./...          # static analysis
go build ./...        # compile
```

Test coverage includes: scheduler state machine (`Run`), ICS parsing, state persistence and corruption handling, executor login/retry logic (token caching, 401 re-auth, retry exhaustion), Discord webhook payload, and config validation.

Linting is configured in `.golangci.yml` (bodyclose, errcheck, staticcheck, noctx, and more). Run locally with:

```bash
golangci-lint run
```
