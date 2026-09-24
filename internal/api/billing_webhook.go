package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/laststate/trace/internal/billing"
)

// billingWebhook handles POST /v1/billing/webhook — the realtime receiver for
// billing-service v2 outbound events (invoice.paid, subscription.canceled,
// usage.limit_exceeded, ...). It verifies X-LastState-Signature over the exact
// raw bytes, then acknowledges synchronously (202). Entitlement state itself
// keeps flowing through the HMAC admin API
// (POST /v1/admin/organizations/{id}/entitlements), so this endpoint never
// writes directly — it is the live signal that lets the UI, quotas and usage
// rollups react within seconds instead of polling.
//
// Configure with TRACE_BILLING_HMAC_SECRET (must match the secret registered
// via POST billing-service /v1/webhook-endpoints).
func (s *Server) billingWebhook(w http.ResponseWriter, r *http.Request) {
	secret := strings.TrimSpace(os.Getenv("TRACE_BILLING_HMAC_SECRET"))
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unreadable body"})
		return
	}
	if err := billing.VerifyWebhookSignature(secret, raw, r.Header.Get("X-LastState-Signature")); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid signature"})
		return
	}
	var evt billing.BillingEvent
	if err := json.Unmarshal(raw, &evt); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid event JSON"})
		return
	}
	if s.Log != nil {
		s.Log.Info("billing event received",
			"type", evt.Type,
			"org", evt.OrganizationID.String(),
			"event", r.Header.Get("X-LastState-Event"),
		)
	}
	// Synchronous ack; heavy work (quota refresh, usage rollup) happens
	// downstream via the queue/worker so webhook delivery never blocks billing.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":   true,
		"type": evt.Type,
	})
}
