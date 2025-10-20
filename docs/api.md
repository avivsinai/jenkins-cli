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
  "schemaVersion": "1.0",
  "items": [
    {
      "id": "team/app/main/128",
      "number": 128,
      "status": "completed",
      "result": "FAILURE",
      "durationMs": 45000,
      "startTime": "2025-08-12T18:24:03Z",
      "branch": "main",
      "commit": "abc123def456",
      "fields": {
        "parameters": {
          "CHART_NAME": "nova-video-prod"
        }
      }
    }
  ],
  "groups": [
    {
      "key": "param.CHART_NAME",
      "value": "nova-video-prod",
      "count": 3,
      "last": {
        "id": "team/app/main/128",
        "number": 128,
        "status": "completed",
        "result": "FAILURE",
        "durationMs": 45000,
        "startTime": "2025-08-12T18:24:03Z"
      }
    }
  ],
  "nextCursor": "g2wAAAABbQAAAGp...",
  "metadata": {
    "filters": {
      "available": ["result", "status", "branch", "param.*", "artifact.*"],
      "operators": ["=", "!=", "~", ">=", "<="]
    },
    "parameters": [
      {
        "name": "CHART_NAME",
        "isSecret": false,
        "sampleValues": ["nova-video-prod"],
        "frequency": 1
      }
    ],
    "fields": ["number", "result", "parameters"],
    "selection": ["parameters"],
    "groupBy": "param.CHART_NAME",
    "aggregation": "last"
  }
}
```

`groups` is omitted when no aggregation is requested, and `metadata` is present only when `--with-meta` is supplied.

### 2.3 Run search (`jk search --json`, `jk run search --json`)
```json
{
  "schemaVersion": "1.0",
  "items": [
    {
      "jobPath": "releases/prod/deploy",
      "id": "releases/prod/deploy/582",
      "number": 582,
      "status": "completed",
      "result": "SUCCESS",
      "durationMs": 540000,
      "startTime": "2025-10-14T17:25:12Z",
      "branch": "main",
      "commit": "4af3d8b0",
      "fields": {
        "parameters": {
          "CHART_NAME": "nova-video-prod"
        }
      }
    }
  ],
  "metadata": {
    "folder": "releases",
    "jobGlob": "*/deploy-*",
    "filters": [
      "param.CHART_NAME=nova-video-prod"
    ],
    "since": "2025-10-13T17:25:12Z",
    "jobsScanned": 6,
    "maxScan": 500,
    "selection": ["parameters"]
  }
}
```

### 2.4 Progressive log pointer (`/jk/api/runs/<jobPath>/<build>/logs`)
```json
{
  "text": "Running on linux-agent-1...\n",
  "nextOffset": 1472,
  "hasMore": true
}
```

### 2.5 Trigger acknowledgement (`jk run start --json` or `jk run rerun --json` without `--follow`)
```json
{
  "jobPath": "team/app/main",
  "message": "run requested",
  "queueLocation": "https://jenkins.example/queue/item/1357/"
}
```

When `--follow` is supplied with `--json`/`--yaml`, the CLI suppresses live log output and, after the run finishes, emits the run detail payload described in §2.1 instead of the acknowledgement.

### 2.6 Cancel acknowledgement (`jk run cancel --json`)
```json
{
  "jobPath": "team/app/main",
  "build": 128,
  "action": "term",
  "status": "requested"
}
```

The CLI exits successfully once the cancellation request is accepted by Jenkins; it does not wait for the build to terminate.

### 2.7 Run parameter discovery (`jk run params --json`)
```json
{
  "jobPath": "team/app/main",
  "source": "runs",
  "parameters": [
    {
      "name": "CHART_NAME",
      "type": "string",
      "isSecret": false,
      "sampleValues": ["nova"],
      "frequency": 1
    },
    {
      "name": "API_TOKEN",
      "type": "password",
      "isSecret": true,
      "frequency": 1
    }
  ]
}
```

`source` reflects the discovery path (`config`, `runs`, or `auto`), and `sampleValues`/`frequency` are derived from recent runs. Secret parameters omit defaults and sample values.

## 3. Logs

### 3.1 Run log snapshot (`jk log --json`)
```json
{
  "jobPath": "team/app/main",
  "build": 128,
  "status": "completed",
  "result": "SUCCESS",
  "startTime": "2025-08-12T18:24:03Z",
  "duration": "1m32s",
  "log": "Started by user Jane Doe\nRunning on linux-agent-1...\n...",
  "truncated": false
}
```

When the run is still executing the snapshot may be truncated; the `truncated` flag is set to `true` and callers should retry with `--follow` to stream the full log. The `--follow` mode emits live text only and does not support JSON/YAML serialization.

## 4. Credentials

### 4.1 List (`jk cred ls --json` and `/jk/api/credentials`)
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

### 4.2 Create/update request payload
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

## 5. CLI Introspection

### 5.1 Command catalog (`jk help --json`)
```json
{
  "schemaVersion": "1.0",
  "commands": [
    {
      "name": "jk",
      "use": "jk",
      "description": "jk is the Jenkins CLI for developers",
      "flags": [
        {"name": "context", "shorthand": "c", "type": "string", "description": "Active Jenkins context name", "default": "", "persistent": true},
        {"name": "json", "type": "bool", "description": "Output in JSON format when supported", "default": "false", "persistent": true},
        {"name": "yaml", "type": "bool", "description": "Output in YAML format when supported", "default": "false", "persistent": true}
      ],
      "subcommands": [
        {
          "name": "run",
          "use": "run",
          "description": "Interact with job runs",
          "subcommands": [
            {"name": "ls", "use": "run ls <jobPath>", "description": "List recent runs"},
            {"name": "search", "use": "run search", "description": "Search runs across jobs"}
          ]
        }
      ]
    }
  ],
  "exitCodes": {
    "0": "Success",
    "1": "General error",
    "2": "Validation error",
    "3": "Not found",
    "4": "Authentication failure",
    "5": "Permission denied",
    "6": "Connectivity/DNS/TLS failure",
    "7": "Timeout",
    "8": "Feature unsupported"
  }
}
```

## 6. Events (SSE)

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

## 7. Plugin status handshake

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

## 8. Pagination cursors

- Cursors are opaque URL-safe base64 strings produced by the server; clients cannot introspect them.
- Requests accept `cursor=<value>` and `limit=<n>`. Servers may ignore `limit` in favor of their own defaults but must not return more than requested.
- When `nextCursor` is omitted or `null`, the collection is exhausted. Clients may pass `--cursor @prev` to reuse the last seen cursor.

## 9. Enumerations

| Field    | Allowed values                                                       |
|----------|---------------------------------------------------------------------|
| `status` | `queued`, `running`, `completed`                                    |
| `result` | `SUCCESS`, `UNSTABLE`, `FAILURE`, `ABORTED`, `NOT_BUILT`, `null`    |
| `topic`  | `run.started`, `run.progress`, `run.completed`, `queue.entered`, `queue.left`, `node.online`, `node.offline` |

All enumerations are case-sensitive.

---

These schemas back the CLI contract tests and companion plugin REST responses. Update this document and increment the relevant versions before changing any field shape or enumeration.
