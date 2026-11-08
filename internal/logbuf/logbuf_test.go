package logbuf

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileRotationPreservesDayHistory(t *testing.T) {
	dir := t.TempDir()
	day := time.Now().Format("2006-01-02")
	h := &Hub{logDir: dir, out: io.Discard}
	defer func() {
		if h.logFile != nil {
			h.logFile.Close()
		}
	}()
	for i := 0; i < maxDayFiles+2; i++ {
		h.appendToFile(Line{Time: time.Now(), Text: "entry", Level: "info"})
		if i < maxDayFiles+1 {
			if err := h.logFile.Truncate(maxFileBytes); err != nil {
				t.Fatal(err)
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxDayFiles {
		t.Fatalf("got %d files, want %d", len(entries), maxDayFiles)
	}
	// Replace sparse padding with valid JSON so history reading can verify order.
	for i, path := range dayFiles(dir, day) {
		data := []byte(`{"text":"` + string(rune('a'+i)) + `"}` + "\n")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := ReadDay(dir, day)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != maxDayFiles {
		t.Fatalf("got %d lines", len(lines))
	}
	for i, line := range lines {
		if line.Text != string(rune('a'+i)) {
			t.Fatalf("wrong history order: %v", lines)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, day+".5.log")); !os.IsNotExist(err) {
		t.Fatal("excess backup retained")
	}
}
