# `jk` API & JSON Contract Reference

This document is normative for the Jenkins CLI (`jk`) JSON output modes and the companion plugin REST surfaces. Breaking changes to these contracts require a semver-major release of the CLI and/or plugin.

## 1. JSON output conventions
- All timestamps use RFC3339 (`2006-01-02T15:04:05Z07:00`) in UTC unless otherwise noted.
- Optional fields are omitted when empty (`omitempty`); arrays default to `[]`.
- Enumerations are uppercase strings (e.g., `SUCCESS`, `FAILURE`).
- Cursor-based pagination objects follow `{ "items": [...], "nextCursor": "<opaque>" }`. Absent `nextCursor` means the end of the collection.

## 2. Runs

### 2.1 Run detail (`jk run view --json` and `/jk/api/runs/<jobPath>/<build>`)
```json
{
  "id": "team/app/main/128",
  "number": 128,
  "jobPath": "team/app/main",
  "url": "https://jenkins.example/job/team/job/app/job/main/128/",
  "status": "completed",                 // queued | running | completed
  "result": "SUCCESS",                   // SUCCESS | UNSTABLE | FAILURE | ABORTED | NOT_BUILT | null
  "startTime": "2025-08-12T18:24:03Z",
  "durationMs": 92345,
  "estimatedDurationMs": 110000,
  "parameters": [
    {"name": "version", "value": "1.2.3"},
    {"name": "deploy", "value": true}
  ],
  "scm": {
    "branch": "refs/heads/main",
    "commit": "abc123def456",
    "repo": "github.com/org/app",
    "author": "jane@example.com"
  },
  "causes": [
    {"type": "user", "userId": "jane", "userName": "Jane Doe"},
    {"type": "scm", "description": "Branch indexing triggered this build"}
  ],
  "stages": [
    {
      "name": "Build",
      "status": "completed",
      "result": "SUCCESS",
      "durationMs": 12000,
      "startTime": "2025-08-12T18:24:10Z",
      "pauseDurationMs": 0
    },
    {
      "name": "Test",
      "status": "completed",
      "result": "UNSTABLE",
      "durationMs": 63000,
      "startTime": "2025-08-12T18:24:22Z",
      "pauseDurationMs": 0
    }
  ],
  "artifacts": [
    {"fileName": "report.xml", "relativePath": "reports/report.xml", "size": 1234},
    {"fileName": "logs.tar.gz", "relativePath": "logs/logs.tar.gz", "size": 98765}
  ],
  "tests": {"total": 420, "failed": 3, "skipped": 5},
  "queue": {"id": 1357, "queuedAt": "2025-08-12T18:23:55Z"},
  "node": {"displayName": "linux-agent-1", "executor": 0}
}
```

### 2.2 Run list (`jk run ls --json` and `/jk/api/runs`)
```json
{
  "items": [
    {
      "id": "team/app/main/128",
      "number": 128,
      "status": "completed",
      "result": "FAILURE",
      "durationMs": 45000,
      "startTime": "2025-08-12T18:24:03Z",
      "branch": "main",
      "commit": "abc123def456"
    },
    {
      "id": "team/app/main/127",
      "number": 127,
      "status": "completed",
      "result": "SUCCESS",
      "durationMs": 42000,
      "startTime": "2025-08-11T11:12:30Z",
      "branch": "main",
      "commit": "112233445566"
    }
  ],
  "nextCursor": "g2wAAAABbQAAAGp..."
}
```

### 2.3 Progressive log pointer (`/jk/api/runs/<jobPath>/<build>/logs`)
```json
{
  "text": "Running on linux-agent-1...\n",
  "nextOffset": 1472,
  "hasMore": true
}
```

## 3. Credentials

### 3.1 List (`jk cred ls --json` and `/jk/api/credentials`)
```json
{
  "items": [
    {
      "id": "slack-token",
      "type": "secretText",
      "scope": "folder",
      "path": "team/app",
      "description": "Slack notifier",
      "updatedAt": "2025-09-03T12:44:00Z"
    },
    {
      "id": "docker-hub",
      "type": "usernamePassword",
      "scope": "system",
      "description": "Docker Hub service account",
      "updatedAt": "2025-08-01T09:10:11Z"
    }
  ],
  "nextCursor": null
}
```

### 3.2 Create/update request payload
```json
{
  "scope": "folder",
  "folderPath": "team/app",
  "type": "secretText",
  "id": "slack-token",
  "description": "Slack notifier token",
  "data": {
    "secret": "****"
  }
}
```

## 4. Events (SSE)

- Endpoint: `/jk/events/stream?topics=run,queue,node`
- Frames are JSON objects encoded as UTF-8 text events.

```json
{
  "topic": "run.completed",              // run.started | run.progress | queue.entered | node.offline | ...
  "jobPath": "team/app/main",
  "number": 128,
  "status": "completed",
  "result": "SUCCESS",
  "timestamp": "2025-08-12T18:25:35Z"
}
```

## 5. Plugin status handshake

- Endpoint: `GET /jk/api/status`
- Response schema:
```json
{
  "version": "1.0.0",
  "features": ["runs", "credentials", "events"],
  "minClient": "0.3.0",
  "recommendedClient": "1.0.0"
}
```
- Clients send `X-JK-Client: <semver>` and `X-JK-Features: <csv>` headers. If the CLI version is below `minClient`, it must fall back to baseline Jenkins APIs and surface a warning to the user.

## 6. Pagination cursors

- Cursors are opaque URL-safe base64 strings produced by the server; clients cannot introspect them.
- Requests accept `cursor=<value>` and `limit=<n>`. Servers may ignore `limit` in favor of their own defaults but must not return more than requested.
- When `nextCursor` is omitted or `null`, the collection is exhausted. Clients may pass `--cursor @prev` to reuse the last seen cursor.

## 7. Enumerations

| Field    | Allowed values                                                       |
|----------|---------------------------------------------------------------------|
| `status` | `queued`, `running`, `completed`                                    |
| `result` | `SUCCESS`, `UNSTABLE`, `FAILURE`, `ABORTED`, `NOT_BUILT`, `null`    |
| `topic`  | `run.started`, `run.progress`, `run.completed`, `queue.entered`, `queue.left`, `node.online`, `node.offline` |

All enumerations are case-sensitive.

---

These schemas back the CLI contract tests and companion plugin REST responses. Update this document and increment the relevant versions before changing any field shape or enumeration.
