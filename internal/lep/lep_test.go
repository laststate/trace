package lep_test

import (
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/laststate/trace/internal/lep"
)

func TestValidateAndRoundTrip(t *testing.T) {
	// IDENTITY nested: field 2 device_id = "dev1"
	idVal := []byte{2, 4, 'd', 'e', 'v', '1'}
	payload := lep.EncodeTLVs([]lep.TLV{{Type: 1, Value: idVal}})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeCrash, Architecture: 1, Sequence: 7, EventID: 9}, payload)
	if err != nil {
		t.Fatal(err)
	}
	h, err := lep.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if h.Type != lep.TypeCrash || h.Sequence != 7 || h.EventID != 9 {
		t.Fatalf("header %+v", h)
	}
	p, err := lep.Payload(raw)
	if err != nil {
		t.Fatal(err)
	}
	tlvs, err := lep.ParseTLVs(p)
	if err != nil || len(tlvs) != 1 || tlvs[0].Type != 1 {
		t.Fatalf("tlvs=%v err=%v", tlvs, err)
	}
}

func TestRejectBadMagic(t *testing.T) {
	raw := make([]byte, 32)
	copy(raw, []byte("XXXX"))
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	if _, err := lep.Validate(raw); err == nil {
		t.Fatal("expected error")
	}
}
