# Time-Automation

A lightweight Go application, packaged in an Alpine-based Docker image, for automating time-tracking workflows through API calls.

It randomly schedules work and break start/stop actions within configurable time windows, skips holidays and vacation days via ICS calendar integration, persists state across restarts, and sends Discord webhook notifications.

## Features

- Randomised work and break start times within configurable windows
- Enforces minimum and maximum durations for work and break
- ICS calendar integration to skip public holidays and vacation days
- Persists daily state to JSON — restarts never re-trigger already-completed actions
- Retries API calls up to 5 times; automatically re-authenticates on expired tokens (401)
- Discord webhook notifications for planned times, errors, and day-off events
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

### Docker CLI

```bash
docker run -d \
  --name time-automation \
  --restart unless-stopped \
  --env-file .env \
  -v time-automation-state:/data \
  ghcr.io/audioinj/time-automation:latest
```

### Build from Source

```bash
docker build -t time-automation .
docker run --env-file .env -v time-automation-state:/data time-automation
```

## Configuration

Copy `.env.example` to `.env` and fill in your values.

| Variable | Required | Default | Description |
|---|:---:|---|---|
| `DOMAIN` | Yes | — | Base domain of the time-tracking service (e.g. `service.com`) |
| `SUBDOMAIN` | Yes | — | Subdomain prefix (e.g. `time` → `time.service.com`) |
| `USERNAME` | Yes | — | Login username |
| `PASSWORD` | Yes | — | Login password |
| `WEBHOOK_URL` | No | — | Discord webhook URL for notifications |
| `WORK_DAYS` | No | `1-5` | Weekday range/list (0=Sun … 6=Sat), e.g. `1-5` or `1,3,5` |
| `START_WORK_MIN` | Yes | — | Earliest work start time (`HH:MM`) |
| `START_WORK_MAX` | Yes | — | Latest work start time (`HH:MM`) |
| `START_BREAK_MIN` | Yes | — | Earliest break start time (`HH:MM`) |
| `START_BREAK_MAX` | Yes | — | Latest break start time (`HH:MM`) |
| `MIN_WORK_DURATION` | Yes | — | Minimum total work duration (e.g. `8.5h`) |
| `MAX_WORK_DURATION` | Yes | — | Maximum total work duration (e.g. `9.5h`) |
| `MIN_BREAK_DURATION` | Yes | — | Minimum break duration (e.g. `0.75h`) |
| `MAX_BREAK_DURATION` | Yes | — | Maximum break duration (e.g. `1.25h`) |
| `TASK` | No | — | Optional task label sent with each time entry |
| `HOLIDAY_ADDRESS` | No | — | Public ICS URL for public holidays |
| `VACATION_ADDRESS` | No | — | ICS URL for personal vacation calendar (HTTP Basic Auth) |
| `VACATION_KEYWORD` | No | — | Filter vacation events by keyword (case-insensitive) |
| `VERBOSE` | No | `false` | Enable verbose logging |
| `DRY_RUN` | No | `false` | Skip actual API calls (safe for testing) |
| `STATE_FILE` | No | `/app/state.json` | Path to the JSON state file |

## How It Works

The scheduler ticks every 5 seconds and evaluates the current time against four randomised daily thresholds:

```
START_WORK → START_BREAK → STOP_BREAK → STOP_WORK
```

1. **Daily planning** — On the first tick of a new day, all four times are randomised within the configured windows and saved to state. Subsequent restarts load the same planned times.
2. **Holiday/vacation check** — ICS calendars are fetched once per day and cached locally. If today matches a holiday or vacation event the scheduler skips all actions and sends a notification.
3. **Action gating** — Each action fires exactly once; the persisted state prevents re-triggering on restart.
4. **Duration guards** — Work stop is held back until `MIN_WORK_DURATION` has elapsed. Break stop is held back until `MIN_BREAK_DURATION` has elapsed.
5. **Token refresh** — If the API returns 401, the cached token is discarded and a fresh login is attempted before retrying.

## CI / CD

`.github/workflows/docker-build.yml` runs on every push to `main`:

- Builds the Docker image
- Pushes `:latest` and `:sha-<commit>` to `ghcr.io/audioinj/time-automation`

No additional secrets required — the workflow uses the built-in `GITHUB_TOKEN`.

## Development

```bash
go build ./...
go test ./...
```

Tests cover `isWorkDay`, `randomizeTimeRange`, ICS parsing, state persistence, `NetWorkDuration`, executor dry-run, and Discord webhook payload shape.
