package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// tierItem is the JSON shape of a subscription tier as consumed by the billing
// view. Field names are camelCase to match the frontend Plan interface.
type tierItem struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	PriceCents           int      `json:"priceCents"`
	Currency             string   `json:"currency"`
	MaxDevices           int      `json:"maxDevices"`
	MaxEventsPerDay      int64    `json:"maxEventsPerDay"`
	RetentionDays        int      `json:"retentionDays"`
	MaxAPITokens         int      `json:"maxApiTokens"`
	MaxAlertRules        int      `json:"maxAlertRules"`
	Features             []string `json:"features"`
	IsUnlimitedDevices   bool     `json:"isUnlimitedDevices,omitempty"`
	IsUnlimitedEvents    bool     `json:"isUnlimitedEvents,omitempty"`
	IsUnlimitedRetention bool     `json:"isUnlimitedRetention,omitempty"`
}

// enterpriseTiers mirrors the currently published managed-service catalog.
// Checkout is not wired through Trace yet, so these are informational only.
func enterpriseTiers() []tierItem {
	return []tierItem{
		{
			ID: "pilot", Name: "Pilot", PriceCents: 49900, Currency: "usd",
			MaxDevices: 100, RetentionDays: 90,
			Features: []string{"qualified_board", "signed_updates", "crash_reproduced_slo", "direct_support"},
		},
		{
			ID: "fleet", Name: "Fleet", PriceCents: 199900, Currency: "usd",
			MaxDevices: 1000, RetentionDays: 365,
			Features: []string{"qualified_boards_3", "elf_dwarf_symbolication", "postmortem_generation_customer_llm", "ingest_sla_99_5"},
		},
		{
			ID: "enterprise", Name: "Enterprise", PriceCents: 799900, Currency: "usd",
			MaxDevices: -1, RetentionDays: -1,
			IsUnlimitedDevices: true, IsUnlimitedRetention: true,
			Features: []string{"qualified_boards_5", "ingest_sla_99_9", "commercial_trace_license", "indemnity", "field_application_engineer"},
		},
	}
}

// localTier is the single "everything unlocked" tier shown in local deployment
// mode: unlimited everything, free, no paywall.
func localTier() []tierItem {
	return []tierItem{
		{
			ID: "local", Name: "Everything unlocked", PriceCents: 0, Currency: "usd",
			MaxDevices: -1, MaxEventsPerDay: -1, RetentionDays: -1,
			MaxAPITokens: -1, MaxAlertRules: -1,
			IsUnlimitedDevices: true, IsUnlimitedEvents: true, IsUnlimitedRetention: true,
			Features: []string{"all_features", "unlimited_devices", "unlimited_events", "no_paywall"},
		},
	}
}

// apiBillingTiers returns the list of available subscription tiers.
// The response includes both "items" (frontend contract) and "tiers"
// (backward-compatible alias), plus deployment metadata so the UI can render
// the right state (paywall vs everything unlocked).
func (s *Server) apiBillingTiers(w http.ResponseWriter, r *http.Request) {
	var tiers []tierItem
	if s.Cfg.IsLocal() {
		tiers = localTier()
	} else {
		tiers = enterpriseTiers()
	}

	writeJSON(w, 200, map[string]any{
		"items":              tiers,
		"tiers":              tiers,
		"currency":           "usd",
		"interval":           "month",
		"deployment":         s.Cfg.Deployment,
		"billing_enabled":    !s.Cfg.IsLocal(),
		"checkout_available": false,
	})
}

// apiBillingSubscription returns the current subscription for the org.
func (s *Server) apiBillingSubscription(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.Cfg.IsLocal() {
			// Local deployment: no subscription is sold — the org is on an
			// implicit "local" plan with everything unlocked.
			writeJSON(w, 200, map[string]any{
				"id":                 "local",
				"planId":             "local",
				"plan_id":            "local",
				"status":             "active",
				"provider":           "self_hosted",
				"currentPeriodEnd":   "",
				"current_period_end": "",
				"deployment":         "local",
				"billing_enabled":    false,
			})
			return
		}
		writeErr(w, http.StatusNotImplemented, "billing_unavailable", "subscription data is served by the billing service", false)

	case http.MethodDelete:
		if s.Cfg.IsLocal() {
			writeErr(w, 400, "billing_disabled", "subscriptions are not used in local deployment mode", false)
			return
		}
		writeErr(w, http.StatusNotImplemented, "billing_unavailable", "subscription cancellation is served by the billing service", false)

	default:
		writeErr(w, 405, "method", "GET|DELETE", false)
	}
}

// apiBillingCheckout creates a checkout session for a plan upgrade.
func (s *Server) apiBillingCheckout(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.IsLocal() {
		writeErr(w, 400, "billing_disabled", "billing is not available in local deployment mode — everything is unlocked", false)
		return
	}
	var body struct {
		PlanID   string `json:"plan_id"`
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PlanID == "" {
		writeErr(w, 400, "bad_json", "plan_id required", false)
		return
	}

	provider := body.Provider
	if provider == "" {
		provider = "stripe"
	}

	writeErr(w, http.StatusNotImplemented, "billing_unavailable", "checkout is served by the billing service", false)
}

// apiBillingPlans returns the static plan definitions for the pricing page.
func (s *Server) apiBillingPlans(w http.ResponseWriter, r *http.Request) {
	type plan struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Price     string   `json:"price"`
		Features  []string `json:"features"`
		Highlight bool     `json:"highlight"`
	}

	plans := []plan{}
	if s.Cfg.IsLocal() {
		plans = []plan{
			{
				ID: "local", Name: "Everything unlocked", Price: "Free",
				Features:  []string{"unlimited devices", "unlimited events", "unlimited retention", "no paywall"},
				Highlight: true,
			},
		}
	} else {
		plans = []plan{
			{
				ID: "pilot", Name: "Pilot", Price: "$499/month",
				Features: []string{"100 devices", "1 qualified board", "90-day retention", "signed updates"},
			},
			{
				ID: "fleet", Name: "Fleet", Price: "$1,999/month",
				Features:  []string{"1,000 devices", "3 qualified boards", "1-year retention", "99.5% ingest SLA"},
				Highlight: true,
			},
			{
				ID: "enterprise", Name: "Enterprise", Price: "$7,999/month",
				Features: []string{"unlimited devices", "5-board signed matrix", "99.9% SLA", "commercial Trace license"},
			},
		}
	}

	writeJSON(w, 200, map[string]any{"plans": plans, "interval": "month", "deployment": s.Cfg.Deployment, "billing_enabled": !s.Cfg.IsLocal(), "checkout_available": false})
}

// apiUsageMetrics returns current usage as a flat metrics list in the shape the
// billing view expects: {metrics:[{metric_name, value}]}.
func (s *Server) apiUsageMetrics(w http.ResponseWriter, r *http.Request) {
	tenant, ok := orgFrom(r)
	if !ok || tenant.OrgID == uuid.Nil {
		writeJSON(w, 200, map[string]any{"metrics": []map[string]any{}})
		return
	}
	summary, err := s.Store.UsageSummary(r.Context(), tenant.OrgID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	counters := map[string]int64{}
	for _, c := range summary.Counters {
		counters[c.Kind] = c.Counter
	}
	devices, _ := s.Store.CountOrgDevices(r.Context(), tenant.OrgID)
	metrics := []map[string]any{
		{"metric_name": "events", "value": counters["events"]},
		{"metric_name": "devices", "value": devices},
		{"metric_name": "api_calls", "value": counters["api_calls"]},
		{"metric_name": "releases", "value": counters["releases"]},
		{"metric_name": "issues", "value": counters["issues"]},
	}
	writeJSON(w, 200, map[string]any{"metrics": metrics})
}

// billingOrgQuota returns the quota info for the current org's tier.
func (s *Server) billingOrgQuota(orgID uuid.UUID) (map[string]any, error) {
	// In production, query the tier_quotas table joined with the org's tier
	return map[string]any{
		"tier":               "free",
		"max_devices":        5,
		"max_events_per_day": 1000,
		"retention_days":     30,
		"max_api_tokens":     1,
		"max_alert_rules":    0,
		"symbolication":      true,
		"analytics":          false,
	}, nil
}
