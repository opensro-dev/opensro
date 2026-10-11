package recordcache

import (
	"os"
	"runtime"
	"testing"
)

func TestRecordEvictionAndCleanup(t *testing.T) {
	c, err := New[string](2)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	name := c.file.Name()
	for id := uint32(1); id <= 8; id++ {
		if err := c.Put(id, string(rune('A'+id))); err != nil {
			t.Fatal(err)
		}
	}
	for pass := 0; pass < 2; pass++ {
		for id := uint32(1); id <= 8; id++ {
			got, ok := c.Get(id)
			if !ok || got != string(rune('A'+id)) {
				t.Fatal("eviction changed record")
			}
			if len(c.pages) > 2 {
				t.Fatal("cache exceeded capacity")
			}
		}
	}
	if _, ok := c.Get(9); ok {
		t.Fatal("missing record manufactured")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("backing file remains")
	}
}

/*
================
TestBackingFileNeverOutlivesTheProcess

Where the OS can unlink an open file, the backing file has no name once
New returns, so a process that is killed before Close leaks nothing. The
records stay readable through the open descriptor.
================
*/
func TestBackingFileNeverOutlivesTheProcess(t *testing.T) {
	c, err := New[string](1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	path := c.file.Name()
	if err := c.Put(1, "A"); err != nil {
		t.Fatal(err)
	}
	if got, ok := c.Get(1); !ok || got != "A" {
		t.Fatalf("record after unlink = %q, %v", got, ok)
	}
	_, statErr := os.Stat(path)
	if runtime.GOOS == "windows" {
		// Windows keeps the name until Close removes it.
		if statErr != nil || c.name != path {
			t.Fatalf("windows backing file %v, name %q", statErr, c.name)
		}
		return
	}
	if !os.IsNotExist(statErr) || c.name != "" {
		t.Fatalf("backing file still named after New: %v, name %q", statErr, c.name)
	}
}
