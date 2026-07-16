// Package symbolicate resolves addresses via llvm-symbolizer or addr2line.
package symbolicate

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Frame struct {
	Address  uint64 `json:"address"`
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Inline   bool   `json:"inline,omitempty"`
}

// Resolve tries llvm-symbolizer then *-addr2line. Empty binary list = PATH probe.
func Resolve(artifactPath string, addresses []uint64) ([]Frame, []string, error) {
	if len(addresses) == 0 {
		return nil, nil, nil
	}
	var warns []string
	for _, c := range candidates() {
		frames, err := run(c.bin, c.style, artifactPath, addresses)
		if err != nil {
			warns = append(warns, c.bin+": "+err.Error())
			continue
		}
		return frames, warns, nil
	}
	// fall back to raw addresses
	out := make([]Frame, len(addresses))
	for i, a := range addresses {
		out[i] = Frame{Address: a}
	}
	warns = append(warns, "no symbolizer binary found in PATH")
	return out, warns, nil
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

func run(bin, style, artifactPath string, addresses []uint64) ([]Frame, error) {
	abs, err := filepath.Abs(artifactPath)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var args []string
	if style == "addr2line" {
		args = []string{"-e", abs, "-f", "-C", "-p"}
		for _, a := range addresses {
			args = append(args, fmt.Sprintf("0x%x", a))
		}
	} else {
		args = []string{"--obj=" + abs, "--functions", "--demangle", "--inlines"}
		for _, a := range addresses {
			args = append(args, fmt.Sprintf("0x%x", a))
		}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = filepath.Dir(abs)
	cmd.Env = []string{"PATH=", "LANG=C"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() > 1<<20 {
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
	// path may contain drive letters on Windows: C:\foo:12:3
	parts := strings.Split(v, ":")
	if len(parts) < 2 {
		return v, 0, 0
	}
	// last two may be line/col
	col := 0
	line := 0
	fileEnd := len(parts)
	if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
		col = n
		fileEnd--
		if fileEnd > 0 {
			if n, err := strconv.Atoi(parts[fileEnd-1]); err == nil {
				line = n
				fileEnd--
			} else {
				line = col
				col = 0
			}
		}
	}
	return strings.Join(parts[:fileEnd], ":"), line, col
}
