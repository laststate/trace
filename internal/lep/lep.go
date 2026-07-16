// Package lep is a minimal LEP v1 codec (aligned with laststate/protocol).
package lep

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

const (
	HeaderSize      = 24
	MaxEnvelopeSize = 4 << 20
	Magic           = "LSTP"
	Version1        = 1

	FlagAuthenticated uint8 = 1 << 0
	FlagEncrypted     uint8 = 1 << 1
	FlagAEAD          uint8 = 1 << 2
	FlagTruncated     uint8 = 1 << 3
	FlagCompressed    uint8 = 1 << 4
	KnownFlags              = FlagAuthenticated | FlagEncrypted | FlagAEAD | FlagTruncated | FlagCompressed

	// Event types (header type byte).
	TypeCrash      uint8 = 1
	TypeError      uint8 = 2
	TypeMessage    uint8 = 3
	TypeHealth     uint8 = 4
	TypeReset      uint8 = 5
	TypeLog        uint8 = 6
	TypePeripheral uint8 = 7
	TypeCoredump   uint8 = 8
)

type ErrorKind string

const (
	ErrorCorrupt     ErrorKind = "corrupt"
	ErrorUnsupported ErrorKind = "unsupported"
	ErrorTooLarge    ErrorKind = "too_large"
)

type ValidationError struct {
	Kind   ErrorKind
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Reason
	}
	return e.Field + ": " + e.Reason
}

type Header struct {
	Version       uint8
	Type          uint8
	Architecture  uint8
	Flags         uint8
	Sequence      uint32
	EventID       uint32
	PayloadLength uint32
}

type TLV struct {
	Type  uint16
	Value []byte
}

func Validate(data []byte) (Header, error) {
	var h Header
	if len(data) > MaxEnvelopeSize {
		return h, &ValidationError{Kind: ErrorTooLarge, Field: "envelope", Reason: "exceeds maximum"}
	}
	if len(data) < HeaderSize+4 {
		return h, &ValidationError{Kind: ErrorCorrupt, Field: "envelope", Reason: "too short"}
	}
	if string(data[:4]) != Magic {
		return h, &ValidationError{Kind: ErrorCorrupt, Field: "magic", Reason: "expected LSTP"}
	}
	h = Header{
		Version: data[4], Type: data[5], Architecture: data[6], Flags: data[7],
		Sequence: binary.LittleEndian.Uint32(data[8:12]), EventID: binary.LittleEndian.Uint32(data[12:16]),
		PayloadLength: binary.LittleEndian.Uint32(data[16:20]),
	}
	if h.Version != Version1 {
		return h, &ValidationError{Kind: ErrorUnsupported, Field: "version", Reason: fmt.Sprintf("%d", h.Version)}
	}
	if h.Flags&^KnownFlags != 0 {
		return h, &ValidationError{Kind: ErrorUnsupported, Field: "flags", Reason: fmt.Sprintf("0x%02x", h.Flags)}
	}
	if (h.Flags&FlagEncrypted != 0) != (h.Flags&FlagAEAD != 0) {
		return h, &ValidationError{Kind: ErrorCorrupt, Field: "flags", Reason: "encrypted/AEAD mismatch"}
	}
	if h.Flags&FlagAEAD != 0 && h.Flags&FlagAuthenticated == 0 {
		return h, &ValidationError{Kind: ErrorCorrupt, Field: "flags", Reason: "AEAD requires authenticated"}
	}

	metadata, auth := 0, 0
	if h.Flags&FlagAEAD != 0 {
		metadata, auth = 28, 16
	} else if h.Flags&FlagAuthenticated != 0 {
		auth = 32
	}
	overhead := HeaderSize + metadata + 4 + auth
	if len(data) < overhead || uint64(h.PayloadLength) != uint64(len(data)-overhead) {
		return h, &ValidationError{Kind: ErrorCorrupt, Field: "payload_length", Reason: "size mismatch"}
	}
	if binary.LittleEndian.Uint32(data[20:24]) != crc32.ChecksumIEEE(data[:20]) {
		return h, &ValidationError{Kind: ErrorCorrupt, Field: "header_crc", Reason: "mismatch"}
	}
	crcOff := HeaderSize + metadata + int(h.PayloadLength)
	if binary.LittleEndian.Uint32(data[crcOff:crcOff+4]) != crc32.ChecksumIEEE(data[HeaderSize:crcOff]) {
		return h, &ValidationError{Kind: ErrorCorrupt, Field: "payload_crc", Reason: "mismatch"}
	}
	if h.Flags&FlagEncrypted == 0 && h.Flags&FlagCompressed == 0 {
		if err := validateTLVs(data[HeaderSize+metadata : crcOff]); err != nil {
			return h, err
		}
	}
	return h, nil
}

func Payload(data []byte) ([]byte, error) {
	h, err := Validate(data)
	if err != nil {
		return nil, err
	}
	if h.Flags&(FlagEncrypted|FlagCompressed) != 0 {
		return nil, &ValidationError{Kind: ErrorUnsupported, Field: "flags", Reason: "encrypted/compressed payload not decoded in v0.1"}
	}
	metadata := 0
	if h.Flags&FlagAEAD != 0 {
		metadata = 28
	}
	start := HeaderSize + metadata
	end := start + int(h.PayloadLength)
	return data[start:end], nil
}

func ParseTLVs(payload []byte) ([]TLV, error) {
	if err := validateTLVs(payload); err != nil {
		return nil, err
	}
	var out []TLV
	for off := 0; off < len(payload); {
		t := binary.LittleEndian.Uint16(payload[off : off+2])
		n := int(binary.LittleEndian.Uint16(payload[off+2 : off+4]))
		off += 4
		val := append([]byte(nil), payload[off:off+n]...)
		out = append(out, TLV{Type: t, Value: val})
		off += n
	}
	return out, nil
}

func Encode(h Header, payload []byte) ([]byte, error) {
	if h.Version == 0 {
		h.Version = Version1
	}
	if h.Version != Version1 {
		return nil, &ValidationError{Kind: ErrorUnsupported, Field: "version", Reason: "not 1"}
	}
	if h.Flags&^FlagTruncated != 0 {
		return nil, &ValidationError{Kind: ErrorUnsupported, Field: "flags", Reason: "plain encode only allows TRUNCATED"}
	}
	if err := validateTLVs(payload); err != nil && len(payload) > 0 {
		return nil, err
	}
	raw := make([]byte, HeaderSize+len(payload)+4)
	copy(raw[0:4], Magic)
	raw[4] = h.Version
	raw[5] = h.Type
	raw[6] = h.Architecture
	raw[7] = h.Flags & FlagTruncated
	binary.LittleEndian.PutUint32(raw[8:12], h.Sequence)
	binary.LittleEndian.PutUint32(raw[12:16], h.EventID)
	binary.LittleEndian.PutUint32(raw[16:20], uint32(len(payload)))
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	copy(raw[HeaderSize:], payload)
	binary.LittleEndian.PutUint32(raw[HeaderSize+len(payload):], crc32.ChecksumIEEE(payload))
	return raw, nil
}

func EncodeTLVs(fields []TLV) []byte {
	var out []byte
	for _, f := range fields {
		var hdr [4]byte
		binary.LittleEndian.PutUint16(hdr[0:2], f.Type)
		binary.LittleEndian.PutUint16(hdr[2:4], uint16(len(f.Value)))
		out = append(out, hdr[:]...)
		out = append(out, f.Value...)
	}
	return out
}

func validateTLVs(payload []byte) error {
	for off := 0; off < len(payload); {
		if len(payload)-off < 4 {
			return &ValidationError{Kind: ErrorCorrupt, Field: "payload", Reason: "incomplete TLV"}
		}
		t := binary.LittleEndian.Uint16(payload[off : off+2])
		n := int(binary.LittleEndian.Uint16(payload[off+2 : off+4]))
		off += 4
		if t == 0 {
			return &ValidationError{Kind: ErrorCorrupt, Field: "payload", Reason: "TLV type zero"}
		}
		if n > len(payload)-off {
			return &ValidationError{Kind: ErrorCorrupt, Field: "payload", Reason: "TLV overrun"}
		}
		off += n
	}
	return nil
}

func EventTypeName(t uint8) string {
	switch t {
	case TypeCrash:
		return "crash"
	case TypeError:
		return "error"
	case TypeMessage:
		return "message"
	case TypeHealth:
		return "health"
	case TypeReset:
		return "reset"
	case TypeLog:
		return "log"
	case TypePeripheral:
		return "peripheral"
	case TypeCoredump:
		return "coredump"
	default:
		return "unknown"
	}
}

func ArchName(code uint8) string {
	switch code {
	case 1:
		return "cortex-m"
	case 2:
		return "riscv"
	case 3:
		return "xtensa"
	case 4:
		return "linux"
	default:
		return "unknown"
	}
}
