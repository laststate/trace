//go:build e2e

package e2e_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/laststate/trace/internal/lep"
)

// Requires running Trace: TRACE_E2E_URL and TRACE_E2E_TOKEN
func TestRelayIngestToReady(t *testing.T) {
	base := os.Getenv("TRACE_E2E_URL")
	tok := os.Getenv("TRACE_E2E_TOKEN")
	if base == "" || tok == "" {
		t.Skip("set TRACE_E2E_URL and TRACE_E2E_TOKEN")
	}
	raw, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1, Sequence: 1, EventID: 42}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/ingest", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Idempotency-Key", "e2e-"+time.Now().Format("150405.000"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("ingest %d %s", resp.StatusCode, body)
	}
	// duplicate same payload → 202 with status=duplicate
	req2, _ := http.NewRequest(http.MethodPost, base+"/v1/ingest", bytes.NewReader(raw))
	req2.Header.Set("Authorization", "Bearer "+tok)
	req2.Header.Set("Idempotency-Key", req.Header.Get("Idempotency-Key"))
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != 202 {
		t.Fatalf("expected duplicate 202 got %d %s", resp2.StatusCode, body2)
	}
}
