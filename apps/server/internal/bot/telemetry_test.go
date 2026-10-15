package bot_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/bot"
)

func TestJSONLRecorderWritesOneLinePerEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.jsonl")
	rec, err := bot.NewJSONLRecorder(path, "bot_0001", 0)
	if err != nil {
		t.Fatal(err)
	}
	rec.Record("login", map[string]any{"gid": 7})
	rec.Record("kill", map[string]any{"target": 42})
	rec.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		if row["bot"] != "bot_0001" || row["event"] == nil || row["ts"] == nil {
			t.Fatalf("row = %v", row)
		}
		lines++
	}
	if lines != 2 {
		t.Fatalf("lines = %d, want 2", lines)
	}
}

func TestJSONLRecorderStopsAtTheCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.jsonl")
	rec, err := bot.NewJSONLRecorder(path, "bot_0001", 120)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		rec.Record("decision", map[string]any{"state": "search"})
	}
	rec.Close()
	if !rec.Full() {
		t.Fatal("the recorder did not reach its cap")
	}
	if written := rec.Bytes(); written <= 0 || written > 120 {
		t.Fatalf("bytes = %d, want within (0, 120]", written)
	}
}

// TestJSONLRecorderRotatesAtTheCap guards the rotation: a long run must keep
// its newest records, and the two files together must stay inside the cap.
func TestJSONLRecorderRotatesAtTheCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.jsonl")
	rec, err := bot.NewJSONLRecorder(path, "bot_0001", 400)
	if err != nil {
		t.Fatal(err)
	}
	rec.SetRotate(true)
	for i := 0; i < 100; i++ {
		rec.Record("decision", map[string]any{"n": i})
	}
	rec.Close()
	if rec.Full() {
		t.Fatal("a rotating recorder reported full")
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := os.ReadFile(path + bot.RotatedSuffix)
	if err != nil {
		t.Fatalf("no rotated file: %v", err)
	}
	if len(current) == 0 || len(current) > 200 || len(rotated) == 0 || len(rotated) > 200 {
		t.Fatalf("sizes = %d and %d, want each within (0, 200]", len(current), len(rotated))
	}
	lines := strings.Split(strings.TrimSpace(string(current)), "\n")
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatal(err)
	}
	if last["n"] != float64(99) {
		t.Fatalf("last record n = %v, want the newest record 99", last["n"])
	}
}

func TestNilRecorderIsANoOp(t *testing.T) {
	var rec *bot.JSONLRecorder
	rec.SetRotate(true)
	rec.Record("decision", map[string]any{"state": "search"})
	if rec.Full() {
		t.Fatal("a nil recorder reported full")
	}
	if rec.Bytes() != 0 {
		t.Fatalf("bytes = %d", rec.Bytes())
	}
	rec.Close()
}

func TestRefusalCode(t *testing.T) {
	if code, ok := bot.RefusalCode([]byte{0x02, 0x39}); !ok || code != 0x39 {
		t.Fatalf("refusal = %d %v", code, ok)
	}
	if _, ok := bot.RefusalCode([]byte{0x01, 0x00}); ok {
		t.Fatal("a success body was read as a refusal")
	}
	if _, ok := bot.RefusalCode([]byte{0x02}); ok {
		t.Fatal("a short body was read as a refusal")
	}
}
