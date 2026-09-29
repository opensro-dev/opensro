package gamedata

import (
	"opensro.online/server/internal/testsupport/licensed"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestResolveRefusesInvalidExplicitProjection(t *testing.T) {
	t.Setenv(EnvRoot, t.TempDir())
	t.Setenv(EnvManifestDigest, "")

	_, err := Resolve()
	if err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("invalid projection error = %v", err)
	}
}

func TestResolveUsesShippedProjection(t *testing.T) {
	t.Setenv(EnvRoot, "")
	t.Setenv(EnvManifestDigest, "")

	paths, err := Resolve()
	if err != nil {
		t.Skipf("generated server projection is unavailable: %v", err)
	}
	for name, value := range map[string]string{
		"runtime":             paths.RuntimeRoot,
		"bundle":              paths.BundleRoot,
		"character authority": paths.CharacterAuthorityDir,
		"textdata":            paths.TextdataDir,
		"world authority":     paths.WorldAuthorityDir,
	} {
		if !filepath.IsAbs(value) {
			t.Fatalf("%s path is not absolute: %q", name, value)
		}
	}
}

func TestResolveSharesOneVerifiedImmutableIdentity(t *testing.T) {
	licensed.RequireGameData(t)
	root, manifestDigest := writeTestBundle(t, nil)
	t.Setenv(EnvRoot, root)
	t.Setenv(EnvManifestDigest, manifestDigest)
	t.Setenv(EnvCacheRoot, t.TempDir())

	resolveMu.Lock()
	resolveCache = make(map[string]*resolveResult)
	resolveMu.Unlock()

	const callers = 32
	results := make(chan Paths, callers)
	errors := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			paths, err := Resolve()
			results <- paths
			errors <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for paths := range results {
		// Resolve returns the root through filepath.Abs, which cleans it.
		if paths.BundleRoot != filepath.Clean(root) || paths.ManifestDigest != manifestDigest {
			t.Fatalf("cached paths = %+v, want root %q digest %q", paths, root, manifestDigest)
		}
	}
	resolveMu.Lock()
	entries := len(resolveCache)
	resolveMu.Unlock()
	if entries != 1 {
		t.Fatalf("resolve cache entries = %d, want one immutable identity", entries)
	}
}

func BenchmarkResolveCachedShippedProjection(b *testing.B) {
	b.Setenv(EnvRoot, "")
	b.Setenv(EnvManifestDigest, "")
	resolveMu.Lock()
	resolveCache = make(map[string]*resolveResult)
	resolveMu.Unlock()
	if _, err := Resolve(); err != nil {
		b.Skipf("generated server projection is unavailable: %v", err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := Resolve(); err != nil {
			b.Fatal(err)
		}
	}
}
