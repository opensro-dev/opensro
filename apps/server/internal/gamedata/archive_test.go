package gamedata

import (
	"compress/gzip"
	"encoding/binary"
	"io/fs"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestMaterializeArchiveRoundTripsVerifiedProjection(t *testing.T) {
	licensed.RequireGameData(t)
	root, manifestDigest := writeTestBundle(t, nil)
	archive := filepath.Join(t.TempDir(), "server.srogz")
	writeTestArchive(t, archive, root)
	// materializeArchive returns an absolute, cleaned path; TMP may not be.
	cache := filepath.Clean(t.TempDir())
	t.Setenv(EnvCacheRoot, cache)

	materialized, err := materializeArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := Open(materialized, manifestDigest)
	if err != nil {
		t.Fatalf("open materialized projection: %v", err)
	}
	if bundle.Root != materialized || !strings.HasPrefix(materialized, cache+string(filepath.Separator)) {
		t.Fatalf("unexpected materialized root %q", materialized)
	}

	again, err := materializeArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	if again != materialized {
		t.Fatalf("content-addressed cache changed: %q != %q", again, materialized)
	}
}

func TestMaterializeArchiveRejectsTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "unsafe.srogz")
	writeArchiveRecords(t, archive, []archiveTestRecord{{path: "../escape.txt", contents: []byte("escape")}})
	cache := t.TempDir()
	t.Setenv(EnvCacheRoot, cache)

	_, err := materializeArchive(archive)
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("traversal error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cache, "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("archive escaped extraction root: %v", statErr)
	}
}

type archiveTestRecord struct {
	path     string
	contents []byte
}

func writeTestArchive(t *testing.T, archive, root string) {
	t.Helper()
	var records []archiveTestRecord
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		records = append(records, archiveTestRecord{
			path:     filepath.ToSlash(relative),
			contents: contents,
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(records, func(left, right int) bool { return records[left].path < records[right].path })
	writeArchiveRecords(t, archive, records)
}

func writeArchiveRecords(t *testing.T, archive string, records []archiveTestRecord) {
	t.Helper()
	output, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(output)
	mustWriteArchive(t, compressed, []byte(archiveMagic))
	var count [4]byte
	binary.LittleEndian.PutUint32(count[:], uint32(len(records)))
	mustWriteArchive(t, compressed, count[:])
	for _, record := range records {
		pathBytes := []byte(record.path)
		var header [12]byte
		binary.LittleEndian.PutUint32(header[:4], uint32(len(pathBytes)))
		binary.LittleEndian.PutUint64(header[4:], uint64(len(record.contents)))
		mustWriteArchive(t, compressed, header[:])
		mustWriteArchive(t, compressed, pathBytes)
		mustWriteArchive(t, compressed, record.contents)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustWriteArchive(t *testing.T, output *gzip.Writer, contents []byte) {
	t.Helper()
	if _, err := output.Write(contents); err != nil {
		t.Fatal(err)
	}
}
