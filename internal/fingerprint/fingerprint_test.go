package fingerprint_test

import (
	"strings"
	"testing"

	"github.com/laststate/trace/internal/analysis"
	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/fingerprint"
	"github.com/laststate/trace/internal/lep"
)

func TestFingerprintStable(t *testing.T) {
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		PC:     0x080014a2,
	}
	r := analysis.Report{ProbableCause: "division by zero", Frames: []analysis.Frame{{Address: 0x080014a2}}}
	a := fingerprint.Compute(d, r)
	b := fingerprint.Compute(d, r)
	if a != b || !strings.HasPrefix(a, "v1:") {
		t.Fatalf("fp unstable or bad: %s %s", a, b)
	}
	d2 := d
	d2.PC = 0x08009999
	r2 := analysis.Report{ProbableCause: "division by zero", Frames: []analysis.Frame{{Address: 0x08009999}}}
	if fingerprint.Compute(d2, r2) == a {
		t.Fatal("different PC should change fingerprint")
	}
}
