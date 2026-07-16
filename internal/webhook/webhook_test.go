package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/laststate/trace/internal/webhook"
)

func TestSignVerify(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	ts := "1710000000"
	secret := "sekrit"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if err := webhook.Verify(secret, ts, sig, body); err != nil {
		t.Fatal(err)
	}
	if err := webhook.Verify(secret, ts, "sha256=00", body); err == nil {
		t.Fatal("expected fail")
	}
}
