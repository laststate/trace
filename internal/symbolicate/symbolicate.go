// Package symbolicate resolves addresses via llvm-symbolizer or addr2line (sandboxed).
package symbolicate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	maxAddresses = 64
	maxOutput    = 1 << 20
	defaultTO    = 5 * time.Second
)

type Frame struct {
	Address  uint64 `json:"address"`
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Inline   bool   `json:"inline,omitempty"`
}

// Resolve tries llvm-symbolizer then *-addr2line. Sandbox: abs path, wiped env, timeout, caps.
func Resolve(artifactPath string, addresses []uint64) ([]Frame, []string, error) {
	if len(addresses) == 0 {
		return nil, nil, nil
	}
	if len(addresses) > maxAddresses {
		addresses = addresses[:maxAddresses]
	}
	abs, err := filepath.Abs(artifactPath)
	if err != nil {
		return nil, nil, err
	}
	// refuse path escape / missing file
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		out := rawFrames(addresses)
		return out, []string{"artifact missing for symbolication"}, nil
	}
	var warns []string
	for _, c := range candidates() {
		frames, err := runSandboxed(c.bin, c.style, abs, addresses)
		if err != nil {
			warns = append(warns, c.bin+": "+err.Error())
			continue
		}
		return frames, warns, nil
	}
	warns = append(warns, "no symbolizer binary found in PATH")
	return rawFrames(addresses), warns, nil
}

func rawFrames(addresses []uint64) []Frame {
	out := make([]Frame, len(addresses))
	for i, a := range addresses {
		out[i] = Frame{Address: a}
	}
	return out
}

type cand struct{ bin, style string }

func candidates() []cand {
	var out []cand
	for _, b := range []string{"llvm-symbolizer", "llvm-symbolizer-18", "llvm-symbolizer-17"} {
		if p, err := exec.LookPath(b); err == nil {
			out = append(out, cand{p, "llvm"})
		}
	}
	for _, b := range []string{"arm-none-eabi-addr2line", "riscv-none-elf-addr2line", "addr2line"} {
		if p, err := exec.LookPath(b); err == nil {
			out = append(out, cand{p, "addr2line"})
		}
	}
	return out
}

func runSandboxed(bin, style, absArtifact string, addresses []uint64) ([]Frame, error) {
	// only allow absolute binary from LookPath
	if !filepath.IsAbs(bin) {
		return nil, fmt.Errorf("symbolizer path not absolute")
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTO)
	defer cancel()
	var args []string
	if style == "addr2line" {
		args = []string{"-e", absArtifact, "-f", "-C", "-p"}
		for _, a := range addresses {
			args = append(args, fmt.Sprintf("0x%x", a))
		}
	} else {
		args = []string{"--obj=" + absArtifact, "--functions", "--demangle", "--inlines"}
		for _, a := range addresses {
			args = append(args, fmt.Sprintf("0x%x", a))
		}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	// sandbox: no shell, empty PATH, fixed LANG, workdir = parent of artifact only
	cmd.Dir = filepath.Dir(absArtifact)
	cmd.Env = []string{"LANG=C", "LC_ALL=C", "PATH="}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() > maxOutput {
		return nil, fmt.Errorf("symbolizer output too large")
	}
	return parseOutput(addresses, stdout.String(), style), nil
}

func parseOutput(addresses []uint64, output, style string) []Frame {
	frames := make([]Frame, len(addresses))
	for i, a := range addresses {
		frames[i] = Frame{Address: a}
	}
	lines := strings.Split(output, "\n")
	idx := 0
	if style == "addr2line" {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || idx >= len(frames) {
				continue
			}
			if parts := strings.SplitN(line, " at ", 2); len(parts) == 2 {
				frames[idx].Function = strings.TrimSpace(parts[0])
				frames[idx].File, frames[idx].Line = splitFileLine(parts[1])
				idx++
			}
		}
		return frames
	}
	for i := 0; i < len(lines) && idx < len(frames); i++ {
		fn := strings.TrimSpace(lines[i])
		if fn == "" {
			continue
		}
		frames[idx].Function = fn
		if i+1 < len(lines) {
			f, ln, col := splitFileLineCol(strings.TrimSpace(lines[i+1]))
			frames[idx].File, frames[idx].Line, frames[idx].Column = f, ln, col
			i++
		}
		idx++
	}
	return frames
}

func splitFileLine(v string) (string, int) {
	f, l, _ := splitFileLineCol(v)
	return f, l
}

func splitFileLineCol(v string) (string, int, int) {
	parts := strings.Split(v, ":")
	if len(parts) < 2 {
		return v, 0, 0
	}
	col, line, fileEnd := 0, 0, len(parts)
	if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
		col = n
		fileEnd--
		if fileEnd > 0 {
			if n, err := strconv.Atoi(parts[fileEnd-1]); err == nil {
				line = n
				fileEnd--
			} else {
				line, col = col, 0
			}
		}
	}
	return strings.Join(parts[:fileEnd], ":"), line, col
}
