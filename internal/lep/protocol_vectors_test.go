package lep_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laststate/trace/internal/lep"
)

// TestProtocolVectors runs laststate/protocol test-vectors against this codec.
// Set PROTOCOL_VECTORS to the protocol/test-vectors directory (CI checks out
// the protocol repo). When unset, common sibling layouts are tried; otherwise skip.
func TestProtocolVectors(t *testing.T) {
	root := protocolVectorsRoot()
	if root == "" {
		t.Skip("protocol test-vectors not found; set PROTOCOL_VECTORS")
	}
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Vectors []struct {
			ID     string `json:"id"`
			Path   string `json:"path"`
			Expect string `json:"expect"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Vectors) == 0 {
		t.Fatal("empty manifest")
	}
	for _, v := range manifest.Vectors {
		v := v
		t.Run(v.ID, func(t *testing.T) {
			hexBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(v.Path)))
			if err != nil {
				t.Fatal(err)
			}
			data, err := hex.DecodeString(strings.TrimSpace(string(hexBytes)))
			if err != nil {
				t.Fatal(err)
			}
			_, verr := lep.Validate(data)
			switch v.Expect {
			case "valid":
				if verr != nil {
					t.Fatalf("want valid: %v", verr)
				}
			case "invalid":
				if verr == nil {
					t.Fatal("want invalid")
				}
			default:
				t.Fatalf("unknown expect %q", v.Expect)
			}
		})
	}
}

func protocolVectorsRoot() string {
	if p := os.Getenv("PROTOCOL_VECTORS"); p != "" {
		if st, err := os.Stat(filepath.Join(p, "manifest.json")); err == nil && !st.IsDir() {
			return p
		}
	}
	// Common local layouts: monorepo sibling or CI checkout path.
	candidates := []string{
		filepath.Join("..", "..", "..", "protocol", "test-vectors"),
		filepath.Join("protocol-ref", "test-vectors"),
		filepath.Join("..", "protocol-ref", "test-vectors"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(c, "manifest.json")); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return ""
}
