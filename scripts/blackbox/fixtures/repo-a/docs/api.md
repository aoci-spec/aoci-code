# taskboard-api — endpoint reference

All `/api/*` routes require `Authorization: Bearer <clientId>.<hexHmac>`
(see README "Authentication"). Errors always use the envelope
`{ "error": { "code": string, "message": string } }` with codes
`validation_failed` (400), `bad_request` (400), `unauthorized` (401),
`not_found` (404), `conflict` / `illegal_transition` (409), `internal` (500).

## Health

- `GET /health` → `200 { "status": "ok", "time": iso8601, "uptimeSeconds": number }`
- `GET /health/ready` → `200 { "status": "ready" }`

## Tasks

Task shape:

```json
{
  "id": "task_01J1GZ4Q0RVAHT3M8W2E9XKCPD",
  "title": "Wire CI badge",
  "description": "",
  "status": "todo",
  "priority": "medium",
  "assigneeId": "user_01J1GZ4Q0RVAHT3M8W2E9XKCPD",
  "createdAt": "2026-08-01T09:15:00.000Z",
  "updatedAt": "2026-08-01T09:15:00.000Z"
}
```

- `GET /api/tasks?status=&assigneeId=` → `200 { "tasks": Task[] }`.
  Both filters optional; `status` is one of `todo|doing|done`,
  `assigneeId` must start with `user_`.
- `POST /api/tasks` → `201 Task`. Body: `title` (required, 1-200 chars,
  trimmed), `description` (default `""`, max 2000), `assigneeId` (optional),
  `priority` (`low|medium|high`, default `medium`). New tasks start in `todo`.
- `GET /api/tasks/:id` → `200 Task` or `404`.
- `PATCH /api/tasks/:id` → `200 Task`. Any subset of `title`, `description`,
  `priority`, `assigneeId` (use `null` to unassign); an empty patch is a `400`.
  Status cannot be patched — use the transition endpoint.
- `POST /api/tasks/:id/transition` → `200 Task`. Body `{ "status": "doing" }`.
  Legal moves: `todo→doing`, `doing→done`, `doing→todo`. Anything else is a
  `409 illegal_transition` and leaves the task untouched.
- `DELETE /api/tasks/:id` → `204` or `404`.

## Users

User shape:

```json
{
  "id": "user_01J1GZ4Q0RVAHT3M8W2E9XKCPD",
  "name": "Ada Lovelace",
  "email": "ada@example.com",
  "role": "member",
  "active": true,
  "createdAt": "2026-07-15T08:00:00.000Z"
}
```

- `GET /api/users` → `200 { "users": User[] }`, sorted by name.
- `POST /api/users` → `201 User`. Body: `name` (1-120 chars), `email`
  (validated, lower-cased, unique — duplicates are a `409 conflict`),
  `role` (`admin|member`, default `member`).
- `GET /api/users/:id` → `200 User` or `404`.
- `DELETE /api/users/:id` → `200 User` with `active: false`. Users are
  soft-deleted because tasks keep referencing their ids.

## Conventions

- Ids are prefixed ULID-ish strings: `task_`, `user_`, or `req_` followed by
  26 Crockford-base32 characters; the leading 10 encode the creation time.
- Timestamps are canonical ISO-8601 UTC strings.
- Every response carries an `x-request-id` header for log correlation.
