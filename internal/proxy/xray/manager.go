package xray

import (
	"crypto/sha1" //nolint:gosec // fingerprint only, not security-sensitive
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// binPath is resolved once: XRAY_BIN env var, falling back to "xray" (PATH lookup).
var binPath = func() string {
	if p := os.Getenv("XRAY_BIN"); p != "" {
		return p
	}
	return "xray"
}()

// sidecar tracks one running xray-core process for a single proxy config.
type sidecar struct {
	cmd       *exec.Cmd
	socksAddr string
	configSig string // hash of the vless URI that produced this process — detects config changes
}

// Manager supervises one xray-core subprocess per proxy config id, each with
// its own local SOCKS5 port. Safe for concurrent use.
type Manager struct {
	mu       sync.Mutex
	sidecars map[int]*sidecar
	nextPort int
}

// Default is the package-level sidecar manager singleton.
var Default = &Manager{sidecars: map[int]*sidecar{}, nextPort: 10800}

// EnsureRunning makes sure a healthy xray sidecar is running for proxy config
// id with the given vless:// link, (re)starting it if the link changed or the
// process died, and returns the local "127.0.0.1:port" SOCKS5 address to dial.
func (m *Manager) EnsureRunning(id int, vlessURI string) (string, error) {
	sig := sigOf(vlessURI)

	m.mu.Lock()
	defer m.mu.Unlock()

	if sc, ok := m.sidecars[id]; ok {
		if sc.configSig == sig && sc.cmd.Process != nil && sc.cmd.ProcessState == nil {
			return sc.socksAddr, nil
		}
		m.stopLocked(id)
	}

	link, err := ParseLink(vlessURI)
	if err != nil {
		return "", err
	}

	port := m.nextPort
	m.nextPort++

	cfgJSON, err := BuildConfig(link, port)
	if err != nil {
		return "", fmt.Errorf("build xray config: %w", err)
	}

	cfgPath := filepath.Join(os.TempDir(), fmt.Sprintf("xray-proxy-%d.json", id))
	if err := os.WriteFile(cfgPath, cfgJSON, 0o600); err != nil {
		return "", fmt.Errorf("write xray config: %w", err)
	}

	cmd := exec.Command(binPath, "run", "-c", cfgPath) //nolint:gosec // binPath/cfgPath are server-controlled, not user input
	cmd.Stdout = xrayLogWriter{id: id}
	cmd.Stderr = xrayLogWriter{id: id}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start xray: %w (binary %q not found? set XRAY_BIN)", err, binPath)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	m.sidecars[id] = &sidecar{cmd: cmd, socksAddr: addr, configSig: sig}

	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("xray: proxy #%d sidecar exited: %v", id, err)
		}
	}()

	// Give the process a brief moment to bind its listener before the first
	// caller dials it — xray starts in well under this on any real hardware.
	time.Sleep(300 * time.Millisecond)
	return addr, nil
}

// Stop kills the sidecar for proxy config id, if any (e.g. on delete/disable).
func (m *Manager) Stop(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(id)
}

func (m *Manager) stopLocked(id int) {
	sc, ok := m.sidecars[id]
	if !ok {
		return
	}
	if sc.cmd.Process != nil {
		_ = sc.cmd.Process.Kill()
	}
	delete(m.sidecars, id)
}

func sigOf(s string) string {
	h := sha1.Sum([]byte(s)) //nolint:gosec
	return hex.EncodeToString(h[:])
}

type xrayLogWriter struct{ id int }

func (w xrayLogWriter) Write(p []byte) (int, error) {
	log.Printf("xray[#%d]: %s", w.id, p)
	return len(p), nil
}
