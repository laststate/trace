// Package analysis turns CPU/FAULT TLVs into a readable report.
package analysis

import (
	"encoding/binary"
	"fmt"

	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/lep"
)

type Frame struct {
	Address  uint64 `json:"address"`
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Inline   bool   `json:"inline,omitempty"`
}

type Report struct {
	Architecture     uint8    `json:"architecture"`
	ArchitectureName string   `json:"architecture_name"`
	EventType        uint8    `json:"event_type"`
	PC               uint32   `json:"pc,omitempty"`
	LR               uint32   `json:"lr,omitempty"`
	SP               uint32   `json:"sp,omitempty"`
	CFSR             uint32   `json:"cfsr,omitempty"`
	HFSR             uint32   `json:"hfsr,omitempty"`
	MMFAR            uint32   `json:"mmfar,omitempty"`
	BFAR             uint32   `json:"bfar,omitempty"`
	MCAUSE           uint32   `json:"mcause,omitempty"`
	MEPC             uint32   `json:"mepc,omitempty"`
	MTVAL            uint32   `json:"mtval,omitempty"`
	Causes           []string `json:"causes,omitempty"`
	ProbableCause    string   `json:"probable_cause,omitempty"`
	Frames           []Frame  `json:"frames,omitempty"`
	Confidence       float64  `json:"confidence"`
	Summary          string   `json:"summary"`
	Warnings         []string `json:"warnings,omitempty"`
}

func FromDecoded(d decode.Decoded) Report {
	r := Report{
		Architecture:     d.Header.Architecture,
		ArchitectureName: lep.ArchName(d.Header.Architecture),
		EventType:        d.Header.Type,
		PC:               d.PC,
		LR:               d.LR,
		SP:               d.SP,
		Confidence:       0.4,
	}
	for _, t := range d.TLVs {
		if t.Type == decode.TLVFault {
			applyFault(&r, t.Value, d.Header.Architecture)
		}
	}
	if r.PC != 0 {
		r.Frames = append(r.Frames, Frame{Address: uint64(r.PC &^ 1)})
		r.Confidence += 0.2
	}
	if r.LR != 0 {
		r.Frames = append(r.Frames, Frame{Address: uint64(r.LR &^ 1)})
	}
	if r.ProbableCause != "" {
		r.Confidence += 0.2
	}
	if r.AssertHint(d) {
		r.Confidence += 0.1
	}
	if r.Confidence > 1 {
		r.Confidence = 1
	}
	r.Summary = buildSummary(r, d)
	return r
}

func (r *Report) AssertHint(d decode.Decoded) bool {
	if d.Assert == "" {
		return false
	}
	if r.ProbableCause == "" {
		r.ProbableCause = "assert: " + d.Assert
		r.Causes = append(r.Causes, r.ProbableCause)
	}
	return true
}

func applyFault(r *Report, value []byte, arch uint8) {
	switch arch {
	case 2: // riscv
		if len(value) < 12 {
			r.Warnings = append(r.Warnings, "fault TLV truncated")
			return
		}
		r.MCAUSE = binary.LittleEndian.Uint32(value[0:4])
		r.MEPC = binary.LittleEndian.Uint32(value[4:8])
		r.MTVAL = binary.LittleEndian.Uint32(value[8:12])
		r.Causes, r.ProbableCause = decodeRISCV(r.MCAUSE)
	default:
		if len(value) < 24 {
			r.Warnings = append(r.Warnings, "fault TLV truncated")
			return
		}
		r.CFSR = binary.LittleEndian.Uint32(value[0:4])
		r.HFSR = binary.LittleEndian.Uint32(value[4:8])
		r.MMFAR = binary.LittleEndian.Uint32(value[16:20])
		r.BFAR = binary.LittleEndian.Uint32(value[20:24])
		r.Causes, r.ProbableCause = decodeCortexM(r.CFSR, r.HFSR)
	}
}

func decodeCortexM(cfsr, hfsr uint32) (causes []string, probable string) {
	bits := []struct {
		mask uint32
		text string
	}{
		{1 << 0, "instruction access violation"}, {1 << 1, "data access violation"},
		{1 << 3, "MemManage unstacking error"}, {1 << 4, "MemManage stacking error"},
		{1 << 8, "instruction bus error"}, {1 << 9, "precise data bus error"},
		{1 << 10, "imprecise data bus error"}, {1 << 16, "undefined instruction"},
		{1 << 17, "invalid execution state"}, {1 << 18, "invalid exception return"},
		{1 << 24, "unaligned access"}, {1 << 25, "division by zero"},
	}
	for _, b := range bits {
		if cfsr&b.mask != 0 {
			causes = append(causes, b.text)
		}
	}
	if hfsr&(1<<30) != 0 {
		causes = append(causes, "configurable fault escalated to HardFault")
	}
	if hfsr&(1<<1) != 0 {
		causes = append(causes, "vector table read fault")
	}
	if len(causes) > 0 {
		probable = causes[0]
	}
	return
}

func decodeRISCV(mcause uint32) (causes []string, probable string) {
	interrupt := mcause>>31 != 0
	code := mcause & 0x7fffffff
	if interrupt {
		probable = fmt.Sprintf("interrupt cause %d", code)
		return []string{probable}, probable
	}
	names := map[uint32]string{
		0: "instruction address misaligned", 1: "instruction access fault",
		2: "illegal instruction", 3: "breakpoint",
		4: "load address misaligned", 5: "load access fault",
		6: "store/AMO address misaligned", 7: "store/AMO access fault",
		8: "environment call from U-mode", 11: "environment call from M-mode",
	}
	if name, ok := names[code]; ok {
		return []string{name}, name
	}
	probable = fmt.Sprintf("exception cause %d", code)
	return []string{probable}, probable
}

func buildSummary(r Report, d decode.Decoded) string {
	etype := lep.EventTypeName(d.Header.Type)
	if r.ProbableCause != "" {
		return fmt.Sprintf("%s on %s: %s", etype, r.ArchitectureName, r.ProbableCause)
	}
	if r.PC != 0 {
		return fmt.Sprintf("%s on %s at 0x%08x", etype, r.ArchitectureName, r.PC)
	}
	return fmt.Sprintf("%s on %s", etype, r.ArchitectureName)
}
