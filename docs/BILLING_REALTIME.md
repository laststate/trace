# Trace ↔ billing-service realtime

Trace is the enforcement point: quotas, tiers and the billing UI read live
billing state instead of polling on a timer.

## Receives (push, seconds)

- `POST /v1/billing/webhook` — billing-service v2 events
  (`subscription.created|updated|canceled`, `invoice.paid|failed`,
  `usage.limit_exceeded`, `dunning.escalated`). Verified with
  `X-LastState-Signature: sha256=<hmac(secret, raw-body)>`, acked with 202.
  Heavy work (quota refresh, rollups) goes through the worker queue.
- `POST /v1/admin/organizations/{id}/entitlements` — authoritative tier
  applies (HMAC `X-Billing-Timestamp` + `X-Billing-Signature`, idempotent on
  `billing_event_id`). Unchanged; the webhook above is the live signal, this
  is the state.

## Reads (live, cached)

- `GET billing-service /v1/entitlements/{org}` via
  `internal/billing.RealtimeConfig.FetchEntitlement` (60s cache recommended,
  fail open in `local` mode so a billing outage never blocks ingest).
- `GET billing-service /v1/billing/events/stream` (SSE) via
  `RealtimeConfig.StreamEvents` — same events as the webhook, for services
  that hold a persistent connection.

## Sends

- `POST billing-service /v1/usage` via `RealtimeConfig.ReportUsage`
  (idempotent with `Idempotency-Key: org:metric:period`).

## Configure

```bash
TRACE_BILLING_URL=http://billing:8080
TRACE_BILLING_API_KEY=<bearer>
TRACE_BILLING_HMAC_SECRET=<openssl rand -hex 32>
TRACE_BILLING_ORG_ID=<org-uuid>
```

Register trace as a webhook receiver from the billing side:

```bash
curl -X POST $BILLING_URL/v1/webhook-endpoints \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' -d "{
    \"organization_id\": \"$ORG\",
    \"url\": \"https://trace.internal/v1/billing/webhook\",
    \"secret\": \"$TRACE_BILLING_HMAC_SECRET\",
    \"events\": []
  }"
```

Frontend (`web/src/lib/billing.ts`) reads `/v1/billing/tiers`,
`/v1/billing/subscription` and `/v1/usage` from trace and follows live
updates over `/api/stream` — the browser never holds a billing API key.
