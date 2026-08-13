# taskboard-api

A small REST API for a team task board. Tasks move through a fixed lifecycle
(`todo` -> `doing` -> `done`) with legality checks on every transition, and both
tasks and users live in in-memory stores — there is no database, so restarting
the process resets all state. The service is written in TypeScript on Express,
validates every payload with zod, and logs structured JSON via pino.

## Endpoints

| Method | Path                       | Auth | Description                          |
| ------ | -------------------------- | ---- | ------------------------------------ |
| GET    | `/health`                  | no   | Liveness probe with uptime           |
| GET    | `/health/ready`            | no   | Readiness probe                      |
| GET    | `/api/tasks`               | yes  | List tasks, filter by status/assignee|
| POST   | `/api/tasks`               | yes  | Create a task (starts in `todo`)     |
| GET    | `/api/tasks/:id`           | yes  | Fetch one task                       |
| PATCH  | `/api/tasks/:id`           | yes  | Update title/description/etc.        |
| POST   | `/api/tasks/:id/transition`| yes  | Move a task to a new status          |
| DELETE | `/api/tasks/:id`           | yes  | Delete a task                        |
| GET    | `/api/users`               | yes  | List users                           |
| POST   | `/api/users`               | yes  | Register a user                      |
| GET    | `/api/users/:id`           | yes  | Fetch one user                       |
| DELETE | `/api/users/:id`           | yes  | Deactivate (soft-delete) a user      |

See [docs/api.md](docs/api.md) for request/response shapes and error codes.

## Setup

```bash
npm install
npm run build
AUTH_TOKEN_SECRET="change-me-to-a-long-secret" npm start
```

Run the test suite (vitest) and the type checker:

```bash
npm test
npm run typecheck
```

## Configuration

All configuration comes from environment variables, validated at boot by
`src/config.ts`. The process exits immediately with a descriptive message if
validation fails.

| Variable            | Default | Notes                                        |
| ------------------- | ------- | -------------------------------------------- |
| `PORT`              | `3000`  | TCP port to listen on (1-65535)              |
| `LOG_LEVEL`         | `info`  | One of `fatal error warn info debug trace`   |
| `AUTH_TOKEN_SECRET` | —       | Required, at least 16 characters             |

## Authentication

`/api/*` routes require a bearer token of the form `<clientId>.<hexHmac>`,
where the HMAC is `HMAC-SHA256(AUTH_TOKEN_SECRET, clientId)`. Tokens can be
minted with the exported `signToken` helper in `src/middleware/auth.ts`.

## Seed data

`data/seed.json` holds a realistic starting board (users plus tasks in every
lifecycle state) that operators can POST into a fresh instance.
