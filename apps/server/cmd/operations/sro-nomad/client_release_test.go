/*
===========================================================================

client_release_test.go - publishing browser releases behind the edge

Builds small beta-shaped release packages (hashed files plus release.json
routes) in temporary directories and publishes them into a temporary web
root: materialization, activation, rotation into previous, pruning, a
republish of the active release, and rejection of tampered packages.

===========================================================================
*/
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testReleaseFile struct {
	Path   string `json:"path"`
	Length int64  `json:"length"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"`
}

type testReleaseRoute struct {
	URL      string `json:"url"`
	File     string `json:"file"`
	Offset   int64  `json:"offset"`
	Length   int64  `json:"length"`
	Mime     string `json:"mime"`
	Encoding string `json:"encoding,omitempty"`
}

/*
================
writeClientPackage

Writes a beta-shaped package: the page and bundle as application files, the
pack index as publication.json, and a gzip-encoded data route. `tag` makes
each release's bytes, and so its id, distinct.
================
*/
func writeClientPackage(t *testing.T, tag string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bundle := "assets/index-" + strings.Repeat(tag, 8) + ".js"
	var gzipped bytes.Buffer
	writer := gzip.NewWriter(&gzipped)
	writer.Write([]byte(`{"release":"` + tag + `"}`))
	writer.Close()
	contents := map[string][]byte{
		"application/index.html": []byte(`<script type="module" src="/` + bundle + `"></script>`),
		"application/" + bundle:  []byte("bundle " + tag),
		"publication.json":       []byte(`{"format":"packs","tag":"` + tag + `"}`),
		"data/data.json.gz":      gzipped.Bytes(),
	}
	var files []testReleaseFile
	identity := sha256.New()
	for name, content := range contents {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		files = append(files, testReleaseFile{name, int64(len(content)), hex.EncodeToString(digest[:]), "application"})
		identity.Write(append([]byte(name), content...))
	}
	routes := []testReleaseRoute{
		{URL: "/index.html", File: "application/index.html", Length: int64(len(contents["application/index.html"])), Mime: "text/html"},
		{URL: "/" + bundle, File: "application/" + bundle, Length: int64(len(contents["application/"+bundle])), Mime: "text/javascript"},
		{URL: "/assets/packs/manifest.json", File: "publication.json", Length: int64(len(contents["publication.json"])), Mime: "application/json"},
		{URL: "/assets/data.json", File: "data/data.json.gz", Length: int64(gzipped.Len()), Mime: "application/json", Encoding: "gzip"},
	}
	id := hex.EncodeToString(identity.Sum(nil))
	manifest, err := json.Marshal(map[string]any{"format": clientReleaseFormat, "releaseId": id, "files": files, "routes": routes})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, clientReleaseManifest), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, id
}

/*
================
requireSymlinks

Skips where publishing cannot activate a release (Windows, see
atomicSymlinkSwapSupported); the edge host is Linux.
================
*/
func requireSymlinks(t *testing.T) {
	t.Helper()
	if !atomicSymlinkSwapSupported() {
		t.Skip("release activation needs atomic symlink replacement (POSIX edge host)")
	}
}

/*
================
linkTarget
================
*/
func linkTarget(t *testing.T, webRoot, name string) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(webRoot, name))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

/*
================
servedFile

Reads a URL through the named symlink, as the edge would.
================
*/
func servedFile(t *testing.T, webRoot, link, url string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(webRoot, link, filepath.FromSlash(strings.TrimPrefix(url, "/"))))
	if err != nil {
		t.Fatalf("%s%s: %v", link, url, err)
	}
	return string(body)
}

/*
================
TestPublishClientReleaseMaterializesRotatesAndPrunes
================
*/
func TestPublishClientReleaseMaterializesRotatesAndPrunes(t *testing.T) {
	requireSymlinks(t)
	webRoot := t.TempDir()
	first, firstID := writeClientPackage(t, "a")
	second, secondID := writeClientPackage(t, "b")
	third, thirdID := writeClientPackage(t, "c")

	if _, err := publishClientRelease(first, webRoot, 0); err != nil {
		t.Fatal(err)
	}
	if got := linkTarget(t, webRoot, clientCurrentLink); got != firstID {
		t.Fatalf("client -> %q, want %q", got, firstID)
	}
	if got := linkTarget(t, webRoot, clientPreviousLink); got != "" {
		t.Fatalf("previous exists after the first publish: %q", got)
	}
	// Routes are written at their URL paths; a gzip route is served decoded
	// with its exact bytes kept as the precompressed sidecar.
	if got := servedFile(t, webRoot, clientCurrentLink, "/assets/index-aaaaaaaa.js"); got != "bundle a" {
		t.Fatalf("bundle = %q", got)
	}
	if got := servedFile(t, webRoot, clientCurrentLink, "/assets/data.json"); got != `{"release":"a"}` {
		t.Fatalf("decoded data route = %q", got)
	}
	servedFile(t, webRoot, clientCurrentLink, "/assets/data.json.gz")
	servedFile(t, webRoot, clientCurrentLink, "/release.json")

	if _, err := publishClientRelease(second, webRoot, 0); err != nil {
		t.Fatal(err)
	}
	if linkTarget(t, webRoot, clientCurrentLink) != secondID || linkTarget(t, webRoot, clientPreviousLink) != firstID {
		t.Fatal("second publish must make the first release previous")
	}
	// An open tab of the first release still finds its hashed bundle.
	if got := servedFile(t, webRoot, clientPreviousLink, "/assets/index-aaaaaaaa.js"); got != "bundle a" {
		t.Fatalf("previous bundle = %q", got)
	}

	if _, err := publishClientRelease(third, webRoot, 0); err != nil {
		t.Fatal(err)
	}
	if linkTarget(t, webRoot, clientCurrentLink) != thirdID || linkTarget(t, webRoot, clientPreviousLink) != secondID {
		t.Fatal("third publish must rotate the second release into previous")
	}
	if _, err := os.Stat(filepath.Join(webRoot, clientReleasesDir, firstID)); !os.IsNotExist(err) {
		t.Fatalf("keep 0 must prune the release before previous: %v", err)
	}

	// Republishing the active release changes nothing.
	if _, err := publishClientRelease(third, webRoot, 0); err != nil {
		t.Fatal(err)
	}
	if linkTarget(t, webRoot, clientCurrentLink) != thirdID || linkTarget(t, webRoot, clientPreviousLink) != secondID {
		t.Fatal("republishing the active release must not rotate")
	}
}

/*
================
TestPublishClientReleaseAdoptsAnAbsoluteLiveLink

The first production release was activated by hand with an absolute
symlink. Republishing it must not rotate; the next release must make it
previous.
================
*/
func TestPublishClientReleaseAdoptsAnAbsoluteLiveLink(t *testing.T) {
	requireSymlinks(t)
	webRoot := t.TempDir()
	first, firstID := writeClientPackage(t, "a")
	second, secondID := writeClientPackage(t, "b")
	if _, err := publishClientRelease(first, webRoot, 0); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(webRoot, clientCurrentLink)
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(webRoot, clientReleasesDir, firstID), current); err != nil {
		t.Fatal(err)
	}

	if _, err := publishClientRelease(first, webRoot, 0); err != nil {
		t.Fatal(err)
	}
	if linkTarget(t, webRoot, clientPreviousLink) != "" {
		t.Fatal("republishing the hand-linked release must not rotate")
	}
	if _, err := publishClientRelease(second, webRoot, 0); err != nil {
		t.Fatal(err)
	}
	if linkTarget(t, webRoot, clientCurrentLink) != secondID || linkTarget(t, webRoot, clientPreviousLink) != firstID {
		t.Fatal("the hand-linked release must become previous")
	}
	if got := servedFile(t, webRoot, clientPreviousLink, "/assets/index-aaaaaaaa.js"); got != "bundle a" {
		t.Fatalf("previous bundle = %q", got)
	}
}

/*
================
TestPublishClientReleaseRejectsTamperedPackages

Verification fails before anything is written, so no symlinks are needed.
================
*/
func TestPublishClientReleaseRejectsTamperedPackages(t *testing.T) {
	altered, _ := writeClientPackage(t, "a")
	if err := os.WriteFile(filepath.Join(altered, "publication.json"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := publishClientRelease(altered, t.TempDir(), 0); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("altered file accepted: %v", err)
	}

	unlisted, _ := writeClientPackage(t, "a")
	if err := os.WriteFile(filepath.Join(unlisted, "extra.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := publishClientRelease(unlisted, t.TempDir(), 0); err == nil || !strings.Contains(err.Error(), "unlisted") {
		t.Fatalf("unlisted file accepted: %v", err)
	}
}
