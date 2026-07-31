package kernel

import (
	"context"
	"strings"
	"testing"
)

func fake(stdout, stderr string, exit int) *Kernel {
	return NewWithRunner("kernel", func(_ context.Context, _ string, _ ...string) ([]byte, []byte, int) {
		return []byte(stdout), []byte(stderr), exit
	})
}

func TestScan_Clean(t *testing.T) {
	res, err := fake("scanned\nSOVEREIGN_SCAN_CLEAN\n", "", 0).Scan(context.Background(), "m.wasm", []byte("in"))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !res.Ran || !res.Clean || res.Findings != 0 {
		t.Fatalf("want ran+clean, got %+v", res)
	}
	if res.Summary() != "kernel scan: clean" {
		t.Fatalf("summary: %q", res.Summary())
	}
}

func TestScan_Findings(t *testing.T) {
	res, _ := fake("SOVEREIGN_SCAN_FINDINGS=3\n", "", 1).Scan(context.Background(), "m.wasm", []byte("in"))
	if !res.Ran || res.Clean || res.Findings != 3 {
		t.Fatalf("want ran+3 findings, got %+v", res)
	}
	if res.Summary() != "kernel scan: 3 finding(s)" {
		t.Fatalf("summary: %q", res.Summary())
	}
}

func TestScan_KernelError(t *testing.T) {
	res, _ := fake("", "SOVEREIGN_SCAN_ERROR: escaped the box\n", 2).Scan(context.Background(), "m.wasm", []byte("in"))
	if res.Ran {
		t.Fatal("a kernel error must not count as ran")
	}
	if !strings.Contains(res.Err, "escaped the box") {
		t.Fatalf("want error captured, got %q", res.Err)
	}
}

func TestVerify(t *testing.T) {
	if ok, _ := fake("SOVEREIGN_VERIFY_PASS\n", "", 0).Verify(context.Background(), "m.wasm"); !ok {
		t.Fatal("want verify pass")
	}
	if ok, detail := fake("SOVEREIGN_VERIFY_FAIL: bad\n", "", 1).Verify(context.Background(), "m.wasm"); ok || detail == "" {
		t.Fatalf("want verify fail with detail, got ok=%v detail=%q", ok, detail)
	}
}
