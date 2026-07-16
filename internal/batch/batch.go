// Package batch implements Relay binary batch (LSBT) framing.
package batch

import (
	"encoding/binary"
	"fmt"
)

const Magic = "LSBT"
const Version = 1
const FlagZstd uint8 = 1 << 0

type Event struct {
	EventID string
	Payload []byte
}

func Decode(data []byte) ([]Event, error) {
	if len(data) < 12 || string(data[0:4]) != Magic {
		return nil, fmt.Errorf("invalid binary batch magic")
	}
	if data[4] != Version {
		return nil, fmt.Errorf("unsupported binary batch version %d", data[4])
	}
	if data[5]&FlagZstd != 0 {
		return nil, fmt.Errorf("compressed binary batch not supported yet")
	}
	count := binary.LittleEndian.Uint32(data[8:12])
	off := 12
	out := make([]Event, 0, count)
	for i := uint32(0); i < count; i++ {
		if off+2 > len(data) {
			return nil, fmt.Errorf("truncated event id length")
		}
		idLen := int(binary.LittleEndian.Uint16(data[off : off+2]))
		off += 2
		if off+idLen+4 > len(data) {
			return nil, fmt.Errorf("truncated event id")
		}
		id := string(data[off : off+idLen])
		off += idLen
		n := int(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		if off+n > len(data) {
			return nil, fmt.Errorf("truncated event payload")
		}
		payload := append([]byte(nil), data[off:off+n]...)
		off += n
		out = append(out, Event{EventID: id, Payload: payload})
	}
	return out, nil
}

func Encode(events []Event) ([]byte, error) {
	size := 12
	for _, e := range events {
		if len(e.EventID) > 0xffff {
			return nil, fmt.Errorf("event id too long")
		}
		size += 2 + len(e.EventID) + 4 + len(e.Payload)
	}
	out := make([]byte, size)
	copy(out[0:4], Magic)
	out[4] = Version
	binary.LittleEndian.PutUint32(out[8:12], uint32(len(events)))
	off := 12
	for _, e := range events {
		binary.LittleEndian.PutUint16(out[off:off+2], uint16(len(e.EventID)))
		off += 2
		copy(out[off:], e.EventID)
		off += len(e.EventID)
		binary.LittleEndian.PutUint32(out[off:off+4], uint32(len(e.Payload)))
		off += 4
		copy(out[off:], e.Payload)
		off += len(e.Payload)
	}
	return out, nil
}
