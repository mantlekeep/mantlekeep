// Package kernel wraps the sovereign mantlekeep-kernel binary (the Rust wasmtime sandbox)
// as a typed Go API. Verify a module, or SCAN — run a wasm tool on sample input inside
// the sandbox (memory + fuel caps, no network) and report clean / findings / error.
// This is the EXECUTION side of the local test loop: run a DRAFT tool, then RecordTest.
package kernel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Runner executes the kernel binary. Injected so tests need no real binary; the
// default shells out to the compiled kernel.
type Runner func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, exit int)

// Kernel is a typed handle to the mantlekeep-kernel binary.
type Kernel struct {
	bin     string
	timeout time.Duration
	run     Runner
}

// New builds a kernel handle over the binary at bin.
func New(bin string) *Kernel { return &Kernel{bin: bin, timeout: 15 * time.Second, run: osRun} }

// NewWithRunner injects a Runner (for tests).
func NewWithRunner(bin string, run Runner) *Kernel {
	return &Kernel{bin: bin, timeout: 15 * time.Second, run: run}
}

// Default resolves the kernel binary the way the portal does: env override, else the
// in-repo release build.
func Default() *Kernel { return New(resolveBin()) }

func resolveBin() string {
	if b := os.Getenv("MANTLEKEEP_KERNEL_BIN"); b != "" {
		return b
	}
	return "../mantlekeep-kernel/target/release/mantlekeep-kernel"
}

// Available reports whether the kernel binary exists, so a caller can say so plainly
// instead of pretending a run happened.
func (k *Kernel) Available() bool {
	_, err := os.Stat(k.bin)
	return err == nil
}

// ScanResult is the outcome of running a wasm tool on input in the sandbox.
type ScanResult struct {
	Ran      bool   // executed inside the sandbox without a kernel error
	Clean    bool   // ran and reported no findings
	Findings int    // number of findings (when not clean)
	Report   string // the tool's report (stdout)
	Err      string // first line of the kernel error (when it did not run)
}

// Summary is a one-line evidence string for RecordTest's ref.
func (r ScanResult) Summary() string {
	switch {
	case !r.Ran:
		return "kernel scan error: " + r.Err
	case r.Clean:
		return "kernel scan: clean"
	default:
		return fmt.Sprintf("kernel scan: %d finding(s)", r.Findings)
	}
}

// Scan runs a wasm tool on input inside the sandbox. Per the kernel's scan contract:
// exit 0 = clean, 1 = findings, 2 = error.
func (k *Kernel) Scan(ctx context.Context, modulePath string, input []byte) (ScanResult, error) {
	f, err := os.CreateTemp("", "mantlekeep-scan-in-*")
	if err != nil {
		return ScanResult{}, err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(input); err != nil {
		_ = f.Close() // write already failed; that error is what we return
		return ScanResult{}, err
	}
	// The kernel reads this file by name next, so a failed Close (unflushed input) would
	// feed it truncated data — surface it rather than scanning a half-written file.
	if err := f.Close(); err != nil {
		return ScanResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, k.timeout)
	defer cancel()
	stdout, stderr, exit := k.run(ctx, k.bin, "scan", modulePath, f.Name())
	switch exit {
	case 0:
		return ScanResult{Ran: true, Clean: true, Report: string(stdout)}, nil
	case 1:
		return ScanResult{Ran: true, Findings: parseFindings(stdout), Report: string(stdout)}, nil
	default:
		return ScanResult{Ran: false, Err: firstLine(stderr)}, nil
	}
}

// Verify runs the kernel's verifier on a module (the upload gate).
func (k *Kernel) Verify(ctx context.Context, modulePath string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, k.timeout)
	defer cancel()
	stdout, stderr, _ := k.run(ctx, k.bin, "verify", modulePath)
	if strings.Contains(string(stdout), "SOVEREIGN_VERIFY_PASS") {
		return true, "verified by the sovereign kernel"
	}
	line := firstLine(stdout)
	if line == "" {
		line = firstLine(stderr)
	}
	return false, line
}

func osRun(ctx context.Context, name string, args ...string) ([]byte, []byte, int) {
	var o, e bytes.Buffer
	// args are internal/trusted, not request-derived: name is the operator-set kernel binary
	// (MANTLEKEEP_KERNEL_BIN or the in-repo release path), args are literal subcommands plus
	// internal module/temp-file paths. exec.CommandContext runs no shell, so there is no
	// interpolation surface to inject through.
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- args are internal/trusted, not request-derived; no shell involved
	cmd.Stdout = &o
	cmd.Stderr = &e
	err := cmd.Run()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			exit = -1
		}
	}
	return o.Bytes(), e.Bytes(), exit
}

func parseFindings(stdout []byte) int {
	for _, ln := range strings.Split(string(stdout), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "SOVEREIGN_SCAN_FINDINGS="); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n
			}
		}
	}
	return 0
}

func firstLine(b []byte) string {
	s := string(b)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
