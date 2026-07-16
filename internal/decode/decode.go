// Package decode extracts identity and event metadata from LEP TLVs.
package decode

import (
	"encoding/binary"
	"encoding/hex"
	"strings"

	"github.com/laststate/trace/internal/lep"
)

const (
	TLVIdentity     uint16 = 1
	TLVEvent        uint16 = 3
	TLVCPU          uint16 = 4
	TLVFault        uint16 = 5
	TLVAssert       uint16 = 10
	TLVStack        uint16 = 14
	TLVBuildID      uint16 = 0x0010
	TLVProjectID    uint16 = 0x0011
	TLVReleaseID    uint16 = 0x0012
	TLVFirmwareHash uint16 = 0x0013
)

type Identity struct {
	DeviceID          string `json:"device_id,omitempty"`
	Product           string `json:"product,omitempty"`
	HardwareRevision  string `json:"hardware_revision,omitempty"`
	FirmwareVersion   string `json:"firmware_version,omitempty"`
	BuildID           string `json:"build_id,omitempty"`
	ProjectID         string `json:"project_id,omitempty"`
	ReleaseID         string `json:"release_id,omitempty"`
	FirmwareHash      string `json:"firmware_hash,omitempty"`
	GitCommit         string `json:"git_commit,omitempty"`
}

type EventMeta struct {
	Priority      uint8  `json:"priority"`
	Severity      uint8  `json:"severity"`
	CaptureLevel  uint8  `json:"capture_level"`
	Code          uint32 `json:"code"`
	Fingerprint   uint32 `json:"fingerprint"`
	RepeatCount   uint32 `json:"repeat_count"`
}

type Decoded struct {
	Header   lep.Header `json:"header"`
	Identity Identity   `json:"identity"`
	Event    *EventMeta `json:"event,omitempty"`
	Assert   string     `json:"assert,omitempty"`
	PC       uint32     `json:"pc,omitempty"`
	LR       uint32     `json:"lr,omitempty"`
	SP       uint32     `json:"sp,omitempty"`
	TLVs     []lep.TLV  `json:"-"`
}

func Envelope(raw []byte) (Decoded, error) {
	h, err := lep.Validate(raw)
	if err != nil {
		return Decoded{}, err
	}
	out := Decoded{Header: h}
	payload, err := lep.Payload(raw)
	if err != nil {
		// still return header for unsupported encrypted envelopes
		return out, nil
	}
	tlvs, err := lep.ParseTLVs(payload)
	if err != nil {
		return out, err
	}
	out.TLVs = tlvs
	for _, t := range tlvs {
		switch t.Type {
		case TLVIdentity:
			out.Identity = mergeIdentity(out.Identity, parseIdentity(t.Value))
		case TLVEvent:
			if m := parseEvent(t.Value); m != nil {
				out.Event = m
			}
		case TLVCPU:
			pc, lr, sp := parseCPU(t.Value, h.Architecture)
			out.PC, out.LR, out.SP = pc, lr, sp
		case TLVAssert:
			out.Assert = parseAssertMessage(t.Value)
		case TLVBuildID:
			out.Identity.BuildID = asID(t.Value)
		case TLVProjectID:
			out.Identity.ProjectID = string(t.Value)
		case TLVReleaseID:
			out.Identity.ReleaseID = string(t.Value)
		case TLVFirmwareHash:
			out.Identity.FirmwareHash = strings.ToLower(hex.EncodeToString(t.Value))
		}
	}
	return out, nil
}

func SeverityName(sev uint8) string {
	switch sev {
	case 0:
		return "debug"
	case 1:
		return "info"
	case 2:
		return "warning"
	case 3:
		return "error"
	case 4:
		return "fatal"
	default:
		return "error"
	}
}

func parseIdentity(value []byte) Identity {
	var id Identity
	for off := 0; off+2 <= len(value); {
		fid := value[off]
		n := int(value[off+1])
		off += 2
		if off+n > len(value) {
			break
		}
		s := string(value[off : off+n])
		off += n
		switch fid {
		case 1:
			id.ProjectID = s
		case 2:
			id.DeviceID = s
		case 3:
			id.Product = s
		case 4:
			id.HardwareRevision = s
		case 7:
			id.FirmwareVersion = s
		case 8:
			id.BuildID = strings.ToLower(s)
		case 10:
			id.GitCommit = s
		}
	}
	return id
}

func mergeIdentity(a, b Identity) Identity {
	if b.DeviceID != "" {
		a.DeviceID = b.DeviceID
	}
	if b.Product != "" {
		a.Product = b.Product
	}
	if b.HardwareRevision != "" {
		a.HardwareRevision = b.HardwareRevision
	}
	if b.FirmwareVersion != "" {
		a.FirmwareVersion = b.FirmwareVersion
	}
	if b.BuildID != "" {
		a.BuildID = b.BuildID
	}
	if b.ProjectID != "" {
		a.ProjectID = b.ProjectID
	}
	if b.ReleaseID != "" {
		a.ReleaseID = b.ReleaseID
	}
	if b.FirmwareHash != "" {
		a.FirmwareHash = b.FirmwareHash
	}
	if b.GitCommit != "" {
		a.GitCommit = b.GitCommit
	}
	return a
}

func parseEvent(value []byte) *EventMeta {
	if len(value) < 28 {
		return nil
	}
	return &EventMeta{
		Priority:     value[0],
		Severity:     value[1],
		CaptureLevel: value[2],
		Code:         binary.LittleEndian.Uint32(value[7:11]),
		Fingerprint:  binary.LittleEndian.Uint32(value[15:19]),
		RepeatCount:  binary.LittleEndian.Uint32(value[19:23]),
	}
}

func parseCPU(value []byte, arch uint8) (pc, lr, sp uint32) {
	if len(value) < 2 {
		return
	}
	cpuArch := value[0]
	if cpuArch == 0 {
		cpuArch = arch
	}
	// Latch multi-arch container: specials start after 2 + 128 register bytes = 130
	if len(value) >= 138 {
		// specials: lr@130, pc@134; msp often at 126 within register block end
		lr = binary.LittleEndian.Uint32(value[130:134])
		pc = binary.LittleEndian.Uint32(value[134:138])
		if len(value) >= 142 {
			// msp is specials[3] = offset 130+12 = 142? specials order: lr,pc,xpsr,msp,...
			// lr@130, pc@134, xpsr@138, msp@142
			if len(value) >= 146 {
				sp = binary.LittleEndian.Uint32(value[142:146])
			}
		}
		return
	}
	if len(value) >= 12 {
		pc = binary.LittleEndian.Uint32(value[4:8])
		lr = binary.LittleEndian.Uint32(value[8:12])
	}
	return
}

func parseAssertMessage(value []byte) string {
	// line u32 | expr_hash u32 | file_hash u32 | optional nested strings
	if len(value) < 12 {
		return ""
	}
	off := 12
	var parts []string
	for off+2 <= len(value) {
		fid := value[off]
		n := int(value[off+1])
		off += 2
		if off+n > len(value) {
			break
		}
		s := string(value[off : off+n])
		off += n
		if fid >= 1 && fid <= 3 && s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

func asID(value []byte) string {
	if printable(value) {
		return strings.ToLower(strings.TrimSpace(string(value)))
	}
	return strings.ToLower(hex.EncodeToString(value))
}

func printable(value []byte) bool {
	if len(value) == 0 {
		return false
	}
	for _, b := range value {
		if b < 0x20 || b > 0x7e {
			return false
		}
	}
	return true
}
