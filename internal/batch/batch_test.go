package batch_test

import (
	"testing"

	"github.com/laststate/trace/internal/batch"
)

func TestRoundTrip(t *testing.T) {
	in := []batch.Event{
		{EventID: "evt_a", Payload: []byte("LSTP...")},
		{EventID: "evt_b", Payload: []byte{1, 2, 3}},
	}
	raw, err := batch.Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := batch.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].EventID != "evt_a" || string(out[1].Payload) != "\x01\x02\x03" {
		t.Fatalf("%+v", out)
	}
}
