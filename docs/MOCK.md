# TRACE_MOCK — preview mode (`TRACE_MOCK=true`)

Sem DB/S3/fila/workers. Tudo em memória e determinístico (charts ancorados em
`2026-08-15T10:30:00Z` em `internal/mockdata/mockdata.go:now()`); auth bypass —
toda request é admin (`mockdata.Me/Login`).

## Ligar

```bash
TRACE_MOCK=true go run ./cmd/trace
set TRACE_MOCK=true && go run .\cmd\trace   # Windows
./scripts/mock-preview.sh / scripts\mock-preview.bat
```

Wiring: `internal/config/config.go:MockMode`, `cmd/trace/main.go`,
`internal/api/mock_handler.go:MockServer/Handler`.

## Escopo (o que é mockado)

- Implementado em `mock_handler.go:Handler`: `GET /health/live|/ready`,
  `/api/overview|/stream(SSE)|/issues|/issues/{id}|/events|/devices|/releases|/artifacts|/alerts|/channels|/relays|/projects|/hardware|/boots|/jobs/dead|/audit|/search|/me`,
  `POST /api/auth/login|/logout`, `POST /api/auth/register|/signup → 201`,
  OIDC login/callback → redirect `/`, `GET /v1/relay/capabilities`,
  `GET /openapi.json` (mínimo), billing local
  (`GET /v1/billing/tiers|/subscription|/plans`, `POST /v1/billing/checkout → 400 billing_disabled`,
  `GET /v1/usage` fixo), ingest accept-only
  (`POST /v1/ingest|/events|/events:batch|/artifacts|/relay/heartbeat`),
  `GET /metrics` (`trace_mock_total 0`), SPA fallback com cache
  (`/` no-cache, `/assets/` immutable).
- **Não mockado / difere de prod**: sem persistência (ingest `202 accepted`
  descartado), sem SCIM/SAML/OIDC reais (`capabilities: saml=false scim=false`),
  sem billing real (`billing_enabled=false`, checkout 400), sem fila/worker/
  dunning, `openapi.json` mínimo (só overview/issues).

## Limites

- Nunca usar em produção: sem auth, sem persistência, sem auditoria real.
- Frontend-only: serve para UI/demos/testes de frontend e prototipagem.
- E2E real exige DB + `TRACE_MOCK=false` (ver `docs/E2E.md`).
