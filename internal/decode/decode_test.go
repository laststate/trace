package decode_test

import (
	"testing"

	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/lep"
)

func TestIdentityFromEnvelope(t *testing.T) {
	// nested identity: device_id + firmware_version + build_id
	idVal := []byte{
		2, 5, 'b', 'o', 'a', 'r', 'd',
		7, 5, '1', '.', '2', '.', '3',
		8, 8, 'd', 'e', 'a', 'd', 'b', 'e', 'e', 'f',
	}
	// EVENT severity fatal
	ev := make([]byte, 28)
	ev[1] = 4
	payload := lep.EncodeTLVs([]lep.TLV{
		{Type: decode.TLVIdentity, Value: idVal},
		{Type: decode.TLVEvent, Value: ev},
		{Type: decode.TLVBuildID, Value: []byte("deadbeef")},
	})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeCrash, Architecture: 1, Sequence: 1, EventID: 42}, payload)
	if err != nil {
		t.Fatal(err)
	}
	d, err := decode.Envelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Identity.DeviceID != "board" {
		t.Fatalf("device=%q", d.Identity.DeviceID)
	}
	if d.Identity.FirmwareVersion != "1.2.3" {
		t.Fatalf("fw=%q", d.Identity.FirmwareVersion)
	}
	if d.Identity.BuildID != "deadbeef" {
		t.Fatalf("build=%q", d.Identity.BuildID)
	}
	if d.Event == nil || d.Event.Severity != 4 {
		t.Fatalf("event=%+v", d.Event)
	}
}
