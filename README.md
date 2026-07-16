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

## v0.3 (skipped backlog)

- Binary batch LSBT (`binary_batch: true`)
- MinIO/S3 object store (`TRACE_S3_*`)
- Alerts + HMAC webhooks
- OIDC login (`TRACE_OIDC_*`)
- Symbolizer sandbox (timeout, env wipe, caps)
- React UI (Vite) in `web/`

SAML: not implemented — use OIDC; add when ADFS/SAML-only IdP required.

## License

AGPL-3.0 (see `LICENSE`).
