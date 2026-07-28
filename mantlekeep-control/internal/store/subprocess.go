package store

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	mantlekeep "mantlekeep.dev/control"
)

// RegisterSubprocess registers an OUT-OF-PROCESS store driver: a separate binary
// the core launches and talks to over JSON-on-stdio. The driver's dependencies —
// and their CVEs — live in that binary's own artifact, never linked into the
// core. This is the host isolation posture: a third-party CVE blocks (at most)
// this one adapter binary, which is patched and scanned on its own, while the
// core keeps shipping. It is also cross-platform — a subprocess works on Windows,
// unlike Go's native .so plugins.
func RegisterSubprocess(name, binPath string) {
	Register(name, func(_ string) (mantlekeep.Store, error) { return newSubprocess(binPath) })
}

type subReq struct {
	Op     string `json:"op"`
	Key    string `json:"key,omitempty"`
	Value  string `json:"value,omitempty"`
	Prefix string `json:"prefix,omitempty"`
}

type subResp struct {
	OK    bool     `json:"ok"`
	Value string   `json:"value,omitempty"`
	Keys  []string `json:"keys,omitempty"`
	Err   string   `json:"err,omitempty"`
}

// Subprocess is a mantlekeep.Store backed by an external driver process.
type Subprocess struct {
	mu  sync.Mutex
	cmd *exec.Cmd
	in  io.Writer
	out *bufio.Scanner
}

func newSubprocess(binPath string) (*Subprocess, error) {
	cmd := exec.Command(binPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch driver %q: %w", binPath, err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &Subprocess{cmd: cmd, in: stdin, out: sc}, nil
}

// call sends one request and reads one response, serialised by the mutex.
func (s *Subprocess) call(req subReq) (subResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(req)
	if _, err := fmt.Fprintln(s.in, string(b)); err != nil {
		return subResp{}, fmt.Errorf("write to driver: %w", err)
	}
	if !s.out.Scan() {
		return subResp{}, fmt.Errorf("driver closed the connection")
	}
	var resp subResp
	if err := json.Unmarshal(s.out.Bytes(), &resp); err != nil {
		return subResp{}, fmt.Errorf("bad driver reply: %w", err)
	}
	if resp.Err != "" {
		return resp, fmt.Errorf("%s", resp.Err)
	}
	return resp, nil
}

// Put implements mantlekeep.Store.
func (s *Subprocess) Put(_ context.Context, key string, value []byte) error {
	_, err := s.call(subReq{Op: "put", Key: key, Value: base64.StdEncoding.EncodeToString(value)})
	return err
}

// Get implements mantlekeep.Store.
func (s *Subprocess) Get(_ context.Context, key string) ([]byte, error) {
	resp, err := s.call(subReq{Op: "get", Key: key})
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(resp.Value)
}

// List implements mantlekeep.Store.
func (s *Subprocess) List(_ context.Context, prefix string) ([]string, error) {
	resp, err := s.call(subReq{Op: "list", Prefix: prefix})
	if err != nil {
		return nil, err
	}
	return resp.Keys, nil
}

// Close stops the driver process.
func (s *Subprocess) Close() error {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}
