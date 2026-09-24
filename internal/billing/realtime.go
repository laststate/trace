package billing

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RealtimeConfig is the shared contract for talking to billing-service live.
// Env (see .env.example):
//
//	TRACE_BILLING_URL          e.g. http://billing:8080 (stack) or http://localhost:8081 (local)
//	TRACE_BILLING_API_KEY      Bearer key for billing-service /v1
//	TRACE_BILLING_HMAC_SECRET  shared secret for X-LastState-Signature verification
//	TRACE_BILLING_ORG_ID       default org for usage reports / SSE tail
type RealtimeConfig struct {
	URL        string
	APIKey     string
	HMACSecret string
	OrgID      uuid.UUID
	Client     *http.Client
}

// ConfigFromEnv loads the realtime billing config. Empty URL = billing disabled
// (local mode keeps everything unlocked and never dials out).
func ConfigFromEnv() RealtimeConfig {
	var org uuid.UUID
	if raw := strings.TrimSpace(os.Getenv("TRACE_BILLING_ORG_ID")); raw != "" {
		if parsed, err := uuid.Parse(raw); err == nil {
			org = parsed
		}
	}
	return RealtimeConfig{
		URL:        strings.TrimRight(strings.TrimSpace(os.Getenv("TRACE_BILLING_URL")), "/"),
		APIKey:     strings.TrimSpace(os.Getenv("TRACE_BILLING_API_KEY")),
		HMACSecret: strings.TrimSpace(os.Getenv("TRACE_BILLING_HMAC_SECRET")),
		OrgID:      org,
		Client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled reports whether live billing calls should be attempted.
func (c RealtimeConfig) Enabled() bool { return c.URL != "" && c.APIKey != "" }

// BillingEvent mirrors billing-service v2 + SSE payloads.
type BillingEvent struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	OrganizationID uuid.UUID      `json:"organization_id"`
	Data           map[string]any `json:"data,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// VerifyWebhookSignature checks X-LastState-Signature ("sha256=<hex hmac(secret, raw-body)>").
// It uses constant-time comparison and accepts the exact raw bytes received.
func VerifyWebhookSignature(secret string, rawBody []byte, got string) error {
	if secret == "" {
		return fmt.Errorf("billing: no webhook secret configured")
	}
	if got == "" {
		return fmt.Errorf("billing: missing signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(got)) {
		return fmt.Errorf("billing: signature mismatch")
	}
	return nil
}

// ReportUsage POSTs meter deltas to billing-service idempotently.
// key should be stable per (org, metric, period) so replays dedupe server-side.
func (c RealtimeConfig) ReportUsage(ctx context.Context, orgID uuid.UUID, metrics map[string]int64, key string) error {
	if !c.Enabled() {
		return nil
	}
	payload := map[string]any{
		"organization_id": orgID.String(),
		"metrics":         metrics,
		"idempotency_key": key,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/v1/usage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("billing: usage report returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// FetchEntitlement GETs the live tier for an org (proxied by billing-service
// from the trace admin API). Callers should cache for ~60s and fail open in
// local mode so a billing outage never blocks ingest.
func (c RealtimeConfig) FetchEntitlement(ctx context.Context, orgID uuid.UUID) (map[string]any, error) {
	if !c.Enabled() {
		return map[string]any{"tier": "local", "billing_enabled": false}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/v1/entitlements/"+orgID.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("billing: entitlement fetch returned HTTP %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// StreamEvents tails GET /v1/billing/events/stream (SSE) and invokes onEvent
// for every billing event until ctx ends. It reconnects with backoff on
// transient disconnects; slow consumers are fine because the channel is
// drained line-by-line here.
func (c RealtimeConfig) StreamEvents(ctx context.Context, events []string, onEvent func(BillingEvent)) error {
	if !c.Enabled() {
		<-ctx.Done()
		return ctx.Err()
	}
	url := c.URL + "/v1/billing/events/stream"
	if c.OrgID != uuid.Nil {
		url += "?organization_id=" + c.OrgID.String()
		if len(events) > 0 {
			url += "&events=" + strings.Join(events, ",")
		}
	} else if len(events) > 0 {
		url += "?events=" + strings.Join(events, ",")
	}
	backoff := time.Second
	for {
		if err := c.streamOnce(ctx, url, onEvent); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c RealtimeConfig) streamOnce(ctx context.Context, url string, onEvent func(BillingEvent)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("billing: stream returned HTTP %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var eventType, data string
	flush := func() {
		if data == "" {
			eventType = ""
			return
		}
		var evt BillingEvent
		if err := json.Unmarshal([]byte(data), &evt); err == nil {
			if evt.Type == "" {
				evt.Type = eventType
			}
			onEvent(evt)
		}
		eventType, data = "", ""
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // heartbeat
		}
		if v, ok := strings.CutPrefix(line, "event:"); ok {
			eventType = strings.TrimSpace(v)
			continue
		}
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			chunk := strings.TrimSpace(v)
			if data != "" {
				data += "\n"
			}
			data += chunk
		}
	}
	return scanner.Err()
}
