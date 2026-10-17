/*
===========================================================================

telemetry.go - the structured JSONL record of one bot run

Each bot writes one JSON object per line to a file on the data volume. A
record names the action, the target, the position, the result with the
refusal reason, HP and MP, the kill, the drop and the latency. The writer
stops at a byte cap, so a run cannot fill the data volume.

Phase 3 of issue #74 (issue #77).

===========================================================================
*/
package bot

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// DefaultTelemetryMaxBytes caps one telemetry file (64 MiB).
const DefaultTelemetryMaxBytes int64 = 64 << 20

// JSONLRecorder appends one JSON object per line to a file. The zero value is
// not usable. A nil *JSONLRecorder is a no-op, so a caller can pass nil to
// disable the telemetry.
type JSONLRecorder struct {
	mu   sync.Mutex
	file *os.File
	// path is the file name, for the rotation. rotate selects the rotation
	// at the cap. See SetRotate.
	path   string
	rotate bool
	bot      string
	maxBytes int64
	written  int64
	full     bool
}

// NewJSONLRecorder opens the file in append mode and counts its current size.
// The file is owner-only (0600). maxBytes <= 0 selects the default cap.
func NewJSONLRecorder(path, botName string, maxBytes int64) (*JSONLRecorder, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultTelemetryMaxBytes
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &JSONLRecorder{file: file, path: path, bot: botName, maxBytes: maxBytes, written: info.Size()}, nil
}

// RotatedSuffix is the name suffix of the rotated telemetry file.
const RotatedSuffix = ".1"

// SetRotate selects what the recorder does at the cap. Without the rotation
// the recorder stops and drops every later line. With the rotation the
// recorder renames the file to path + RotatedSuffix and starts a new file, so
// a long run keeps its newest records. A file then holds half of the cap, so
// the two files together stay inside the cap.
func (r *JSONLRecorder) SetRotate(rotate bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.rotate = rotate
	r.mu.Unlock()
}

// rotateLocked replaces the rotated file with the current file and opens a
// new one. The caller holds the lock. It reports false when the new file
// could not be opened. The recorder is then closed.
func (r *JSONLRecorder) rotateLocked() bool {
	_ = r.file.Close()
	r.file = nil
	if err := os.Rename(r.path, r.path+RotatedSuffix); err != nil {
		return false
	}
	file, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false
	}
	r.file = file
	r.written = 0
	return true
}

// Record writes one line. The timestamp and the bot name are added here, so
// the caller passes only the event fields. A full writer drops the line.
func (r *JSONLRecorder) Record(event string, fields map[string]any) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full || r.file == nil {
		return
	}
	line := make(map[string]any, len(fields)+3)
	line["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	line["bot"] = r.bot
	line["event"] = event
	for key, value := range fields {
		line[key] = value
	}
	data, err := json.Marshal(line)
	if err != nil {
		return
	}
	data = append(data, '\n')
	limit := r.maxBytes
	if r.rotate {
		limit = r.maxBytes / 2
	}
	if r.written+int64(len(data)) > limit {
		if !r.rotate || !r.rotateLocked() {
			r.full = true
			return
		}
	}
	if _, err := r.file.Write(data); err != nil {
		return
	}
	r.written += int64(len(data))
}

// Full reports whether the byte cap was reached.
func (r *JSONLRecorder) Full() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.full
}

// Bytes reports the number of bytes written.
func (r *JSONLRecorder) Bytes() int64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.written
}

// Close closes the file. A second call is a no-op.
func (r *JSONLRecorder) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
}
