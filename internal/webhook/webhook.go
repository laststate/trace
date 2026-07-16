// Package webhook delivers signed outbound notifications.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type Delivery struct {
	StatusCode int
	Body       string
	Success    bool
}

// Send posts JSON with HMAC headers. Timestamp + body signed to prevent replay.
func Send(ctx context.Context, targetURL, secret string, payload any) (Delivery, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Delivery{}, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := sign(secret, ts, raw)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return Delivery{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Last-State-Timestamp", ts)
	req.Header.Set("X-Last-State-Signature", "sha256="+sig)
	req.Header.Set("User-Agent", "laststate-trace/0.2")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Delivery{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	return Delivery{StatusCode: resp.StatusCode, Body: string(b), Success: ok}, nil
}

func sign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify is for receivers/tests.
func Verify(secret, ts, sigHeader string, body []byte) error {
	want := "sha256=" + sign(secret, ts, body)
	if !hmac.Equal([]byte(want), []byte(sigHeader)) {
		return fmt.Errorf("bad signature")
	}
	return nil
}
