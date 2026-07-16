# End-to-end testing

## Package tests

```bash
go test ./internal/... -count=1
cd web && npm ci && npm test && npm run build
```

## UI smoke (Vite + mocks)

```bash
cd web && npm run test:e2e:install && npm run test:e2e
```

These tests cover routing, branding, and dashboard layout. They do not replace a live Trace stack.

## Full stack (release gate)

1. Start Postgres and Trace (`docker compose up --build`).
2. Export credentials:

```bash
export TRACE_E2E_URL=http://127.0.0.1:8080
export TRACE_E2E_TOKEN="$(tr -d '\n' < data/bootstrap-token.txt)"
```

3. Ingest smoke:

```bash
go test -tags=e2e ./scripts -count=1 -v
# or
go run ./scripts/smoke_ingest.go
```

4. Optional: configure Relay destination to `TRACE_E2E_URL` with the same bearer token; send LEP from Latch or a directory source.

5. Confirm worker processing and UI: open `/overview` and `/issues`.

### Expected path

```
Latch LEP → Relay → POST /v1/ingest
  → object store + events row
  → queue.Jober (postgres/redis)
  → worker process_event
  → issue (+ notify)
  → UI APIs
```
