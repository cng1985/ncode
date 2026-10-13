# NCode

NCode is a Chinese-language project collaboration and delivery platform. This first implementation is a local development slice for project work and acceptance, based on the supplied SRS v4.0. The requirements document is marked “待评审”; the implementation is not a production release or a P0 acceptance claim.

## Current slice

- Create and list projects with a goal, slug, type, visibility, and calculated progress.
- Create and edit work items with an owner, priority, due date, acceptance checklist, and change history.
- Move work through backlog, ready, in progress, review, acceptance, and accepted states.
- Require every current acceptance criterion to be checked before accepting a work item. Writes carry `expectedVersion`; stale writes return HTTP 409. Project and work-item creation support `Idempotency-Key` replay.
- Use the same interface on desktop and mobile widths.

The dashboard's project progress is derived from work-item states: backlog/canceled 0%, ready 20%, in progress 50%, review 75%, pending business acceptance 90%, accepted 100%, averaged across the project's items. An empty project shows 0%.

The current slice does not include authentication, tenant authorization, Git hosting, pull request execution, isolated workspaces or CI, learning evaluation, evidence sharing, recruiting, or release/deployment. Other navigation entries show an explicit not-yet-connected state. The API and frontend bind to loopback by default; the API rejects non-loopback bind addresses. Do not expose this development build to a network or use it with real candidate, company, or production data.

## Stack

- Go 1.27.2, Gin, GORM, Fx
- PostgreSQL 17 for project, work-item, checklist, activity, and idempotency records
- Redis 7 is included in Compose for the next queue/coordination slice; the current slice does not depend on it
- Vue 3, Vite, Element Plus, and JavaScript

## Backend modules

The Fx composition root lives in internal/app. It combines config, logging, database, API, and HTTP server modules; cmd/api only starts that application. The database module owns connection checks, schema migration, demo seeding, and connection-pool cleanup. The HTTP server module binds during Fx startup, reports bind errors to startup, and gracefully shuts down before the database closes.

## Configuration

The application reads server and PostgreSQL settings from configs/config.yaml. Set CONFIG_FILE to load another YAML file. DATABASE_PASSWORD can override the password in YAML, and DATABASE_URL can override the complete PostgreSQL URL for deployment environments. API_ADDR, APP_ENV, and SEED_DEMO can also override their YAML values. Production always disables demo seeding.

The checked-in YAML contains credentials for the loopback-only Compose database and is for local development only. Keep real deployment credentials outside version control.

## Start locally

Start PostgreSQL and Redis from the repository root:

```sh
cd /workspace/ncode
docker compose up -d --wait postgres redis
```

In one terminal, start the API:

```sh
cd /workspace/ncode
export PATH=/workspace/.tools/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build
export GOPATH=/workspace/.cache/go
go run ./cmd/api
```

In another terminal, start the frontend:

```sh
cd /workspace/ncode/web
npm run dev
```

Open `http://127.0.0.1:5173`. The API uses the local Compose database at `127.0.0.1:54329` by default, and seeds three demonstration projects and work items into an empty database. Set `SEED_DEMO=false` to turn off seeding. The local Compose database password is for development only.

## Verify

```sh
cd /workspace/ncode
export PATH=/workspace/.tools/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build
export GOPATH=/workspace/.cache/go
go test ./...
go vet ./...
cd /workspace/ncode/web
npm run build
```

Automated tests cover environment validation, the Fx dependency graph, and HTTP server startup/shutdown. During this setup, the API was also exercised against a temporary PostgreSQL database through project creation, idempotent replay/conflict, work-item transitions, acceptance checklist enforcement, stale-version conflict, paging, and activity history. The browser flow created a project and work item, completed the lifecycle, and checked the desktop/mobile workbench.

## API shape

Business actions use `POST /api/v1/{resource}/{action}` and return `{ "code", "msg", "data" }`. Paginated responses also include root-level `page`, `size`, `total`, `totalPage`, and `responseType`. IDs are safe JavaScript integers; state is numeric with a `stateName`; dates use `yyyy-MM-dd`; update actions carry `expectedVersion`. Project/work-item creation requires `Idempotency-Key`.

Available routes include:

- `POST /api/v1/project/create`, `project/query/page`, `project/query/one`
- `POST /api/v1/workItem/create`, `workItem/update`, `workItem/transition`, `workItem/accept`
- `POST /api/v1/workItem/query/page`, `workItem/query/activity`
- `GET /healthz`
