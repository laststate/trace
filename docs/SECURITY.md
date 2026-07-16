# Security

## Defaults

| Setting | Default | Notes |
|---------|---------|--------|
| `TRACE_ALLOW_PUBLIC_REGISTER` | `false` | Viewer-only if enabled |
| `TRACE_OIDC_AUTO_JOIN` | `false` | Invite/membership required |
| `TRACE_SAML_INSECURE` | `false` | Unsigned ACS disabled |
| `TRACE_TRUSTED_PROXIES` | empty | XFF ignored unless peer matches |
| `TRACE_SECRETS_KEY` | empty | AES-GCM for channel/alert secrets when set |
| `TRACE_COOKIE_SECURE` | auto | `true` when `TRACE_PUBLIC_URL` is https |

## Production

```bash
export TRACE_ENV=production
export TRACE_OPEN_UI=false
export TRACE_ALLOW_PUBLIC_REGISTER=false
export TRACE_OIDC_AUTO_JOIN=false
export TRACE_SAML_INSECURE=false
export TRACE_TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12
export TRACE_SECRETS_KEY="$(openssl rand -base64 32)"
export TRACE_QUEUE=postgres   # or redis
```

`ValidateProduction()` rejects open UI, public register, SAML insecure, and OIDC auto-join.

## Sessions

Browser sessions use an **HttpOnly** cookie (`trace_session`). Bearer tokens remain supported for Relay and scripts. Prefer cookies for the SPA (`credentials: 'same-origin'`).

## Feature maturity

| Feature | Status |
|---------|--------|
| Password login + scopes | Supported |
| OIDC (PKCE, JWKS) | Supported; membership required |
| SAML ACS | Experimental; not production-ready |
| SCIM | Not implemented (HTTP 501) |

## Webhooks

Outbound webhooks validate URLs (SSRF protection). Delivery retries use exponential backoff.
