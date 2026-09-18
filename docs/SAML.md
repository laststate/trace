# SAML SSO — implementação, lib e fallback

Status: **experimental, suportado em Trace; fora de escopo em billing-service**
(auth é centralizado no Trace; billing-service consome entitlements via Admin API).

## Decisão de lib (auditada ou fallback documentado)

- Usado: `github.com/russellhaering/goxmldsig v1.6.1` + `github.com/beevik/etree v1.7.0`
  (diretas em `go.mod`; verificação de assinatura envelopada + canonicalização
  exclusiva + digest + RSA/ECDSA em `internal/saml/saml.go:verifySignature`).
- Avaliado e **não adotado**: `crewjam/saml` (SP+IdP completo, padrão de mercado).
  Motivo: superfície maior (bindings, metadados, sessões, IdP) do que o mínimo
  necessário agora (SP metadata + ACS + Conditions). Reavaliar quando for preciso
  suportar matriz ampla de IdPs (ver "Limitações").
- Auditoria: `goxmldsig` é lib pequena, single-maintainer, **sem auditoria externa
  formal**. Mitigações adotadas:
  - `Strict=true` no parser XML + rejeição de assertion sem assinatura por padrão.
  - Enforcement de `Conditions` (audience, recipient, `NotBefore`/`NotOnOrAfter`,
    `SubjectConfirmation` expiry, skew 5min) em `ParseResponse`.
  - Testes de tamper: `internal/saml/saml_test.go:TestSignedRoundTrip`
    (troca `signed@example.com`→`mallory@example.com` deve falhar) + rejeição de
    audience expirada/garbage/sem-cert.
  - `govulncheck` + Trivy + SBOM em CI (ver `SECURITY.md`).
- Fallback documentado (caminho insecure só para teste local):
  - `VerifyOpts.AllowNoCert` ↔ `TRACE_SAML_INSECURE` (default `false`).
  - Sem certificado: ACS responde `501 saml_not_ready`, salvo teste local explícito.
  - `ValidateProduction()` rejeita `TRACE_SAML_INSECURE=true` em produção.
  - `SECURITY.md`, `README.md` e `.env.example:168` marcam como experimental.

## Wiring

- `internal/api/enterprise.go:apiSAMLMetadata|apiSAMLLogin|apiSAMLACS|apiSAMLConfig`
- `internal/store/saml_analytics.go:GetSAMLConfig|UpsertSAMLConfig` (`saml_configs`)
- `internal/config/config.go:SAMLInsecure`, `TRACE_SAML_INSECURE`
- Rotas: `GET /saml/metadata`, `GET /saml/login`, `POST /saml/acs`,
  `GET|PUT /api/saml/config` (admin).

## Limitações (fora de escopo)

- Matriz completa de IdPs (`Full SAML IdP matrix` em `docs/GAPS_FILLED.md`).
- SLO/logout, AuthnRequest assinado (`AuthnRequestsSigned=false` no metadata).
- `billing-service` não tem SAML (só menção de plano em `docs/PRICING.md`,
  `docs/ENTITLEMENTS.md` — auth via Trace).

## Operação

```bash
# IdP cert obrigatório; insecure só local
export TRACE_SAML_INSECURE=false
```

Teste: `go test ./internal/saml -run TestSignedRoundTrip -v`
