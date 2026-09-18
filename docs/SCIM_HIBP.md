# SCIM Groups mínimo + HIBP breach check

## SCIM v2 (mínimo)

- Base: `internal/api/scim.go`, rotas em `internal/api/server.go:118-129`,
  store em `internal/store/auth.go:198-352`, migração
  `internal/db/migrations/025_scim_groups.sql`.
- Users: `GET/POST /scim/v2/Users`, `GET/PUT/PATCH/DELETE /scim/v2/Users/{id}`
  (`filter=userName eq`, `startIndex/count`, `active=false` = deprovision).
- Groups (flat, sem nesting — RFC 7644 §4.2 MAY):
  `GET/POST /scim/v2/Groups`, `GET/PUT/PATCH/DELETE /scim/v2/Groups/{id}`
  (`filter=displayName eq`, paginação; `PUT/PATCH` troca só `members`;
  `displayName` imutável — rename via delete+create; `DELETE` cascade).
- Auth: `requireUI(..., "admin")` + `scimScopeOrg(?organization_id)` (membro da org).
- OpenAPI: `/scim/v2/Users`, `/scim/v2/Users/{id}`, `/scim/v2/Groups`,
  `/scim/v2/Groups/{id}` em `internal/api/openapi.go`.
- Auditoria: `auth.scim_group_create` com IP/UA.

## HIBP breach check

- `internal/store/phase2.go:CheckPasswordPolicy` (12–128 chars + blocklist
  local `breachedPasswords` + `breachedViaHIBP()`).
- `breachedViaHIBP`: k-anonymity — só prefixo SHA-1 (5 hex) sai do processo;
  `GET https://api.pwnedpasswords.com/range/{prefix5}`, timeout 3s,
  `User-Agent: laststate-trace-pwcheck`, cap 1MB, **fail-open** (falha de rede
  nunca bloqueia onboarding; blocklist local continua valendo).
- Opt-in: `HIBP_CHECK=true` (default `false`; ver `.env.example`).
- Cobertura: `signup` (`api/auth_endpoints.go:apiSignup`), `register`
  (`api/product.go:apiRegister` via `CreateUser`), `invite-accept`
  (via `AcceptInvite→CreateUser`), `reset-password` (`apiResetPassword`);
  `Login` não rejeita (correto); `EnsureAdmin` agora usa `CheckPasswordPolicy`
  (antes só `len>=12`); SCIM cria `passwordless` (sem política — correto).
- Operação: habilitar com `HIBP_CHECK=true`; em ambiente sem egress, manter
  `false` (blocklist local cobre o mínimo).
