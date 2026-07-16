package symbolicate_test

import (
	"testing"

	"github.com/laststate/trace/internal/symbolicate"
)

func TestParseLLVMOutput(t *testing.T) {
	// exercise via Resolve with no binary → raw frames
	frames, warns, err := symbolicate.Resolve("/nonexistent", []uint64{0x8000, 0x9000})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[0].Address != 0x8000 {
		t.Fatalf("%+v", frames)
	}
	if len(warns) == 0 {
		t.Fatal("expected warning about missing symbolizer")
	}
}
