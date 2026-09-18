# Sessões (IP/UA + idle) + onboarding + trilha e2e

## Sessões

- Schema `sessions` (`002_phase2` + `010_usage` + `026_session_activity` +
  `027_session_activity_backfill`):
  `id, user_id, token_hash, prefix, expires_at (+7d), created_at, revoked_at,
  ip INET, user_agent TEXT, last_used_at TIMESTAMPTZ`.
  Novas sessões inserem `last_used_at=now()` (`MintSession`); linhas legadas
  têm backfill `last_used_at=created_at` (027).
- Todo request autenticado (`sessionFrom`) checa idle e faz touch
  fire-and-forget (2s): `TouchSession → last_used_at=now(), ip, user_agent`
  (se ip+ua vazios, só `last_used_at`). `GET /api/me` usa `sessionFrom`
  (idle enforced; antes bypassava via `AuthSession` direto).
- Idle: `TRACE_SESSION_IDLE_TIMEOUT` (default `24h`; `0` desabilita),
  enforcement **lazy por-request** (sem sweeper): expirada → revoke
  best-effort + `401`. Distinto de `TRACE_HTTP_IDLE_TIMEOUT_SEC=120s`
  (HTTP keep-alive) e cookie `MaxAge 7d` / `expires_at +7d` (vida absoluta).
- `X-Forwarded-For`/`X-Real-IP` só vale se peer ∈ `TRACE_TRUSTED_PROXIES`
  (`middleware.go:ConfigureTrustedProxies`).
- Gestão: `GET /api/me/sessions`, `DELETE /api/me/sessions/{id}`,
  `POST /api/me/sessions/revoke-others` (`tenancy_endpoints.go`,
  `openapi.go`, `store/ops.go:ListSessions/Revoke*`).
- Testes: `session_test.go:TestSessionFrom_IdleExpired|IdleFreshTouchesIPUA`
  (+ base `NoServer/NoSecret/FromContext/HeaderAuth`).

## Onboarding (backend; sem wizard UI)

- API (`requireAuth`): `GET /api/onboarding/status`,
  `POST /api/onboarding/step {step}`, `GET /api/onboarding/progress`
  (`onboarding_endpoints.go`, rotas em `server.go`, `openapi.go tags:[auth]`).
- Passos válidos: `signup, org_create, invite_members, first_event, mfa_setup`
  (total 5; `Progress 0–1`, `Percentage 0–100`).
- Store idempotente: `onboarding_steps(user_id, step UNIQUE)` +
  `MarkOnboardingStep ON CONFLICT DO UPDATE` (`store/onboarding.go`,
  migração `011_users_mfa.sql:53-61`).
- UI: `trace/web/src` não tem wizard (grep `onboarding`=0) — alvo futuro;
  backend já sustenta a trilha e2e abaixo.

## Trilha e2e (onboarding + sessões)

```bash
# 1. signup → 201 + cookie/sessão (last_used_at=now, ip/ua no 1º touch)
# 2. GET /api/onboarding/status → progress 1/5 (signup)
# 3. POST /api/onboarding/step org_create → invite_members → first_event (ingest)
#    → mfa_setup → progress 5/5
# 4. GET /api/me/sessions → linha com ip/user_agent/last_used_at
# 5. Idle: TRACE_SESSION_IDLE_TIMEOUT=1s, esperar, request → 401 + revoke
# 6. revoke-others → só sessão atual sobrevive
```

Full-stack existente (ingest→worker→issue→UI) em `docs/E2E.md`; UI smoke
Playwright (`web/e2e/smoke.spec.ts`, `product.spec.ts`) não cobre
sessão/onboarding (mocks) — cobertura aqui é API/store + trilha acima.
Detalhe mock: `docs/MOCK.md` (`TRACE_MOCK=true` bypassa auth).
