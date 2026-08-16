package mihomo

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

// binPath is resolved once: MIHOMO_BIN env var, falling back to "mihomo" (PATH lookup).
var binPath = func() string {
	if p := os.Getenv("MIHOMO_BIN"); p != "" {
		return p
	}
	return "mihomo"
}()

type sidecar struct {
	cmd       *exec.Cmd
	socksAddr string
	configSig string
}

// Manager supervises one mihomo subprocess per proxy config id, each with its
// own local SOCKS5 port. Ports start at 10900 — xray's Manager (internal/proxy/xray)
// uses 10800+, kept in a disjoint range so the two sidecars never collide.
type Manager struct {
	mu       sync.Mutex
	sidecars map[int]*sidecar
	nextPort int
}

// Default is the package-level sidecar manager singleton.
var Default = &Manager{sidecars: map[int]*sidecar{}, nextPort: 10900}

// EnsureRunning makes sure a healthy mihomo sidecar is running for proxy
// config id with the given link/config, (re)starting it if the link changed
// or the process died, and returns the local "127.0.0.1:port" SOCKS5 address.
func (m *Manager) EnsureRunning(id int, rawConfig string) (string, error) {
	sig := sigOf(rawConfig)

	m.mu.Lock()
	defer m.mu.Unlock()

	if sc, ok := m.sidecars[id]; ok {
		if sc.configSig == sig && sc.cmd.Process != nil && sc.cmd.ProcessState == nil {
			return sc.socksAddr, nil
		}
		m.stopLocked(id)
	}

	name, node, err := ParseLink(rawConfig)
	if err != nil {
		return "", err
	}

	port := m.nextPort
	m.nextPort++

	cfgYAML, err := BuildConfig(name, node, port)
	if err != nil {
		return "", fmt.Errorf("build mihomo config: %w", err)
	}

	workDir := filepath.Join(os.TempDir(), fmt.Sprintf("mihomo-proxy-%d", id))
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", fmt.Errorf("mihomo work dir: %w", err)
	}
	cfgPath := filepath.Join(workDir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfgYAML, 0o600); err != nil {
		return "", fmt.Errorf("write mihomo config: %w", err)
	}

	cmd := exec.Command(binPath, "-f", cfgPath, "-d", workDir) //nolint:gosec // binPath/cfgPath are server-controlled, not user input
	cmd.Stdout = mihomoLogWriter{id: id}
	cmd.Stderr = mihomoLogWriter{id: id}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start mihomo: %w (binary %q not found? set MIHOMO_BIN)", err, binPath)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	m.sidecars[id] = &sidecar{cmd: cmd, socksAddr: addr, configSig: sig}

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("mihomo: proxy #%d sidecar exited: %v", id, err)
		}
	}()

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

type mihomoLogWriter struct{ id int }

func (w mihomoLogWriter) Write(p []byte) (int, error) {
	log.Printf("mihomo[#%d]: %s", w.id, p)
	return len(p), nil
}
