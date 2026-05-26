package logbuf

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxLines = 20000
const retainDays = 7

type Line struct {
	Time  time.Time `json:"t"`
	Text  string    `json:"text"`
	Level string    `json:"level"`
}

func detectLevel(text string) string {
	lower := strings.ToLower(text)
	if strings.Contains(lower, ": enriched:") {
		return "success"
	}
	if strings.Contains(lower, "not found in tmdb") ||
		strings.Contains(lower, "skipping") ||
		strings.Contains(lower, "disabled,") ||
		strings.Contains(lower, "reached cutoff") ||
		strings.Contains(lower, "magnet not found") ||
		strings.Contains(lower, "not found after") {
		return "skip"
	}
	if strings.Contains(lower, "error") ||
		strings.Contains(lower, "failed") ||
		strings.Contains(lower, "fatal") ||
		strings.Contains(lower, "panic") {
		return "error"
	}
	return "info"
}

type Hub struct {
	mu      sync.Mutex
	buf     []Line
	subs    map[int]chan Line
	next    int
	out     io.Writer
	logDir  string
	logFile *os.File
	logDay  string
}

var Default = &Hub{
	subs: make(map[int]chan Line),
	out:  os.Stderr,
}

// Init loads today's log file into the in-memory buffer and enables file persistence.
func (h *Hub) Init(logDir string) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logDir = logDir

	// Load today's file so the SSE history covers 00:00→now.
	// Also load yesterday so we don't lose lines near midnight.
	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, t := range []time.Time{now.AddDate(0, 0, -1), now} {
		h.loadFile(filepath.Join(logDir, t.Format("2006-01-02")+".log"), startOfToday.AddDate(0, 0, -1))
	}
	if len(h.buf) > maxLines {
		h.buf = h.buf[len(h.buf)-maxLines:]
	}
	go h.cleanOldFiles()
}

// ReadDay returns all lines for a specific date (YYYY-MM-DD) from the log file.
func ReadDay(logDir, date string) ([]Line, error) {
	path := filepath.Join(logDir, date+".log")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Line{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var lines []Line
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var line Line
		if json.Unmarshal(scanner.Bytes(), &line) == nil {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// loadFile appends lines newer than cutoff to h.buf. Must be called with h.mu held.
func (h *Hub) loadFile(path string, cutoff time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var line Line
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.Time.After(cutoff) {
			h.buf = append(h.buf, line)
		}
	}
}

// appendToFile must be called with h.mu held.
func (h *Hub) appendToFile(line Line) {
	if h.logDir == "" {
		return
	}
	day := line.Time.Format("2006-01-02")
	if day != h.logDay || h.logFile == nil {
		if h.logFile != nil {
			h.logFile.Close()
		}
		f, err := os.OpenFile(filepath.Join(h.logDir, day+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		h.logFile = f
		h.logDay = day
		go h.cleanOldFiles()
	}
	data, _ := json.Marshal(line)
	h.logFile.Write(append(data, '\n')) //nolint:errcheck
}

func (h *Hub) cleanOldFiles() {
	if h.logDir == "" {
		return
	}
	entries, _ := os.ReadDir(h.logDir)
	cutoff := time.Now().AddDate(0, 0, -retainDays).Format("2006-01-02")
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".log") && len(name) >= 10 && name[:10] <= cutoff {
			os.Remove(filepath.Join(h.logDir, name)) //nolint:errcheck
		}
	}
}

// prune drops oldest lines when buffer is over maxLines. Must be called with h.mu held.
func (h *Hub) prune() {
	if len(h.buf) > maxLines {
		h.buf = append([]Line(nil), h.buf[len(h.buf)-maxLines:]...)
	}
}

func (h *Hub) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\n")
	if text != "" {
		line := Line{Time: time.Now(), Text: text, Level: detectLevel(text)}

		h.mu.Lock()
		h.buf = append(h.buf, line)
		h.prune()
		h.appendToFile(line)
		subs := make([]chan Line, 0, len(h.subs))
		for _, ch := range h.subs {
			subs = append(subs, ch)
		}
		h.mu.Unlock()

		for _, ch := range subs {
			select {
			case ch <- line:
			default:
			}
		}
	}
	return h.out.Write(p)
}

func (h *Hub) History() []Line {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Line, len(h.buf))
	copy(out, h.buf)
	return out
}

func (h *Hub) LogDir() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.logDir
}

func (h *Hub) Subscribe() (int, chan Line) {
	ch := make(chan Line, 200)
	h.mu.Lock()
	id := h.next
	h.next++
	h.subs[id] = ch
	h.mu.Unlock()
	return id, ch
}

func (h *Hub) Unsubscribe(id int) {
	h.mu.Lock()
	delete(h.subs, id)
	h.mu.Unlock()
}
