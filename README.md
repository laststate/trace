# Last State Trace

Open-source observability for embedded firmware and hardware.

Vertical path (v0.1):

```
Relay → POST /v1/ingest → raw object storage → PostgreSQL → worker →
decode → device/release → fingerprint → issue → web UI
```

## Quickstart

```bash
docker compose up --build
```

On first boot Trace creates a default organization/project and prints an ingest token (also written to `./data/bootstrap-token.txt`).

Point Relay at Trace:

```yaml
destinations:
  - id: trace
    type: trace
    url: http://localhost:8080
    auth:
      type: bearer
      token: <ingest token>
```

Open http://localhost:8080

## Local (no Docker for Trace binary)

```bash
# postgres required
export TRACE_DATABASE_URL=postgres://trace:trace@localhost:5432/trace?sslmode=disable
export TRACE_OBJECT_DIR=./data/objects
go run ./cmd/trace
```

## Relay contract

- `GET /v1/relay/capabilities`
- `POST /v1/ingest` (`application/octet-stream`, `Authorization: Bearer …`, `Idempotency-Key` / `X-Last-State-Event-ID`)
- `POST /v1/events:batch` (JSON `accepted` / `duplicates` / `rejected`)
- `2xx` and `409` = durable success / duplicate

LEP validation follows [laststate/protocol](https://github.com/laststate/protocol).

## v0.2 (Phase 2 / Production Alpha)

Adds: artifact upload + ELF build-id, external symbolication, issue status,
devices/releases/artifacts UI, session auth + RBAC ranks, Prometheus `/metrics`,
audit log.

Still local/dev defaults: `TRACE_OPEN_UI=true` (GET open), admin `admin@localhost` / `admin`.

## License

AGPL-3.0 (see `LICENSE`).
