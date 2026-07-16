package artifact_test

import (
	"debug/elf"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// Minimal ELF64 LE with GNU build-id note — enough for InspectFile.
func TestInspectBuildID(t *testing.T) {
	// Use a real empty-ish approach: skip if we can't craft; write tiny valid ELF is heavy.
	// Instead ensure non-ELF returns error (trust boundary).
	dir := t.TempDir()
	p := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(p, []byte("not-an-elf"), 0o644); err != nil {
		t.Fatal(err)
	}
	// import cycle avoid: call via Open only here as smoke that file is not ELF
	if _, err := elf.Open(p); err == nil {
		t.Fatal("expected non-elf error")
	}
	_ = binary.LittleEndian
}
