/*
===========================================================================

client_release.go - publish a verified browser release behind the edge

A beta release package (apps/client-next/tools/beta) is a set of hashed
files plus release.json, whose routes map each public URL to a slice of one
of those files. The edge serves plain files, so publishing writes the routes
out as a URL-shaped tree. The web root (default /var/www/opensro) holds:

	releases/<releaseId>/   one materialized release each, never modified
	client   -> releases/<current>
	previous -> releases/<the release before it>

The edge serves /play and /assets from client and falls back to previous
for a file the current release no longer has. A tab that is still open, or
a page cached before the switch, therefore still finds the hashed bundles
and packs it references for one release generation after a publish.

Publishing verifies the package against release.json (every listed file's
length and SHA-256, nothing unlisted, every route in bounds), materializes
it into a staging directory renamed into place, then moves previous and
client with atomic symlink renames, so neither name ever points nowhere.
Releases beyond -keep are pruned; client and previous never are.

===========================================================================
*/
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	clientReleaseFormat     = "sro-beta-release-v1"
	clientReleaseManifest   = "release.json"
	clientCurrentLink       = "client"
	clientPreviousLink      = "previous"
	clientReleasesDir       = "releases"
	defaultClientWebRoot    = "/var/www/opensro"
	defaultClientKeep       = 3
	clientReleaseFileMode   = 0o644
	clientReleaseDirMode    = 0o755
	clientGzipRouteMaxBytes = 128 << 20
)

var clientReleaseIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// clientReleaseRoute maps one public URL to a slice of a package file.
// Encoding "gzip" means the slice is gzip bytes of the identity response;
// Gzip names a separate precompressed sidecar of an identity route.
type clientReleaseRoute struct {
	URL      string `json:"url"`
	File     string `json:"file"`
	Offset   int64  `json:"offset"`
	Length   int64  `json:"length"`
	Mime     string `json:"mime"`
	Encoding string `json:"encoding,omitempty"`
	Gzip     *struct {
		File   string `json:"file"`
		Offset int64  `json:"offset"`
		Length int64  `json:"length"`
	} `json:"gzip,omitempty"`
}

// clientReleaseManifestFile is the part of the beta release.json this
// command relies on (tools/beta/policy.mjs verifyDirectory owns the rest).
type clientReleaseManifestFile struct {
	Format    string `json:"format"`
	ReleaseID string `json:"releaseId"`
	Files     []struct {
		Path   string `json:"path"`
		Length int64  `json:"length"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
	Routes []clientReleaseRoute `json:"routes"`
}

/*
================
runPublishClient
================
*/
func runPublishClient(arguments []string) error {
	flags := flag.NewFlagSet("publish-client", flag.ContinueOnError)
	packageDir := flags.String("package", "", "verified beta release package directory (contains release.json)")
	webRoot := flags.String("web-root", defaultClientWebRoot, "edge web root holding releases/, client and previous")
	keep := flags.Int("keep", defaultClientKeep, "releases retained besides client and previous, newest first")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *packageDir == "" {
		return errors.New("-package is required")
	}
	if *keep < 0 {
		return errors.New("-keep must not be negative")
	}
	id, err := publishClientRelease(*packageDir, *webRoot, *keep)
	if err != nil {
		return err
	}
	fmt.Printf("published client release %s under %s\n", id, *webRoot)
	return nil
}

/*
================
publishClientRelease

Verifies, materializes and activates one release package; returns its id.
Republishing the active release changes nothing.
================
*/
func publishClientRelease(packageDir, webRoot string, keep int) (string, error) {
	manifest, err := verifyClientPackage(packageDir)
	if err != nil {
		return "", err
	}
	releases := filepath.Join(webRoot, clientReleasesDir)
	if err := os.MkdirAll(releases, clientReleaseDirMode); err != nil {
		return "", err
	}
	// A release directory only ever appears by renaming a verified staging
	// tree, so an existing one is the same release and is reused as is.
	target := filepath.Join(releases, manifest.ReleaseID)
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		if err := materializeClientRelease(packageDir, target, manifest); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	current := filepath.Join(webRoot, clientCurrentLink)
	active, err := os.Readlink(current)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read %s: %w", current, err)
	}
	// Compare by release id: a link made by hand may use an absolute target.
	relative := filepath.Join(clientReleasesDir, manifest.ReleaseID)
	if filepath.Base(active) != manifest.ReleaseID {
		if active != "" {
			if err := swapSymlink(filepath.Join(webRoot, clientPreviousLink), active); err != nil {
				return "", err
			}
		}
		if err := swapSymlink(current, relative); err != nil {
			return "", err
		}
	}
	return manifest.ReleaseID, pruneClientReleases(webRoot, keep)
}

/*
================
verifyClientPackage

Checks release.json's identity fields, every listed file's length and
SHA-256, that the package holds nothing unlisted, and that every route
names a listed file within its bounds.
================
*/
func verifyClientPackage(dir string) (clientReleaseManifestFile, error) {
	var manifest clientReleaseManifestFile
	raw, err := os.ReadFile(filepath.Join(dir, clientReleaseManifest))
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, fmt.Errorf("%s: %w", clientReleaseManifest, err)
	}
	if manifest.Format != clientReleaseFormat || !clientReleaseIDPattern.MatchString(manifest.ReleaseID) || len(manifest.Files) == 0 {
		return manifest, errors.New("release.json is not a verified beta release manifest")
	}
	lengths, listed := map[string]int64{}, map[string]bool{}
	for _, file := range manifest.Files {
		if !filepath.IsLocal(filepath.FromSlash(file.Path)) || file.Path == clientReleaseManifest ||
			listed[file.Path] || !clientReleaseIDPattern.MatchString(file.SHA256) || file.Length < 0 {
			return manifest, fmt.Errorf("invalid release entry %q", file.Path)
		}
		digest, length, err := hashFile(filepath.Join(dir, filepath.FromSlash(file.Path)))
		if err != nil {
			return manifest, err
		}
		if length != file.Length || digest != file.SHA256 {
			return manifest, fmt.Errorf("release file %s does not match release.json", file.Path)
		}
		lengths[file.Path], listed[file.Path] = file.Length, true
	}
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if name != clientReleaseManifest && !listed[name] {
			return fmt.Errorf("unlisted release file %s", name)
		}
		return nil
	}); err != nil {
		return manifest, err
	}
	seen := map[string]bool{}
	within := func(file string, offset, length int64) bool {
		return listed[file] && offset >= 0 && length >= 0 && offset+length <= lengths[file]
	}
	for _, route := range manifest.Routes {
		local := strings.TrimPrefix(route.URL, "/")
		if !strings.HasPrefix(route.URL, "/") || !filepath.IsLocal(filepath.FromSlash(local)) || seen[route.URL] ||
			!within(route.File, route.Offset, route.Length) || (route.Encoding != "" && route.Encoding != "gzip") ||
			(route.Gzip != nil && !within(route.Gzip.File, route.Gzip.Offset, route.Gzip.Length)) {
			return manifest, fmt.Errorf("invalid release route %q", route.URL)
		}
		seen[route.URL] = true
	}
	if !seen["/index.html"] || !seen["/assets/packs/manifest.json"] {
		return manifest, errors.New("release routes lack /index.html or /assets/packs/manifest.json")
	}
	return manifest, nil
}

/*
================
materializeClientRelease

Writes every route of a verified package as a file at its URL path in a
staging directory beside target, adds release.json, then renames the
staging directory into place. A gzip-encoded route is written decoded,
with its exact bytes kept as the .gz sidecar the edge serves precompressed;
an identity route's separate gzip sidecar is written the same way.
================
*/
func materializeClientRelease(packageDir, target string, manifest clientReleaseManifestFile) error {
	staging := target + ".staging"
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	routes := map[string]bool{}
	for _, route := range manifest.Routes {
		routes[route.URL] = true
	}
	for _, route := range manifest.Routes {
		path := filepath.Join(staging, filepath.FromSlash(strings.TrimPrefix(route.URL, "/")))
		slice, err := readReleaseSlice(packageDir, route.File, route.Offset, route.Length)
		if err != nil {
			return err
		}
		body := slice
		if route.Encoding == "gzip" {
			reader, err := gzip.NewReader(bytes.NewReader(slice))
			if err != nil {
				return fmt.Errorf("route %s: %w", route.URL, err)
			}
			body, err = io.ReadAll(io.LimitReader(reader, clientGzipRouteMaxBytes+1))
			if err != nil || len(body) > clientGzipRouteMaxBytes {
				return fmt.Errorf("route %s does not decode within budget: %v", route.URL, err)
			}
			if !routes[route.URL+".gz"] {
				if err := writeReleaseFile(path+".gz", slice); err != nil {
					return err
				}
			}
		}
		if err := writeReleaseFile(path, body); err != nil {
			return err
		}
		if route.Gzip != nil && !routes[route.URL+".gz"] {
			sidecar, err := readReleaseSlice(packageDir, route.Gzip.File, route.Gzip.Offset, route.Gzip.Length)
			if err != nil {
				return err
			}
			if err := writeReleaseFile(path+".gz", sidecar); err != nil {
				return err
			}
		}
	}
	manifestBytes, err := os.ReadFile(filepath.Join(packageDir, clientReleaseManifest))
	if err != nil {
		return err
	}
	if err := writeReleaseFile(filepath.Join(staging, clientReleaseManifest), manifestBytes); err != nil {
		return err
	}
	return os.Rename(staging, target)
}

/*
================
swapSymlink

Points link at target atomically: a new symlink is created beside it and
renamed over the old one, so readers see the old or the new target, never
a missing name.
================
*/
func swapSymlink(link, target string) error {
	next := link + ".next"
	if err := os.Remove(next); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Symlink(target, next); err != nil {
		return err
	}
	return os.Rename(next, link)
}

/*
================
pruneClientReleases

Removes release directories other than the client and previous targets,
keeping the newest `keep` of them for rollback.
================
*/
func pruneClientReleases(webRoot string, keep int) error {
	protected := map[string]bool{}
	for _, name := range []string{clientCurrentLink, clientPreviousLink} {
		if target, err := os.Readlink(filepath.Join(webRoot, name)); err == nil {
			protected[filepath.Base(target)] = true
		}
	}
	entries, err := os.ReadDir(filepath.Join(webRoot, clientReleasesDir))
	if err != nil {
		return err
	}
	type candidate struct {
		name    string
		modTime int64
	}
	var candidates []candidate
	for _, entry := range entries {
		if !entry.IsDir() || protected[entry.Name()] || !clientReleaseIDPattern.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		candidates = append(candidates, candidate{entry.Name(), info.ModTime().UnixNano()})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modTime > candidates[j].modTime })
	for index, release := range candidates {
		if index < keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(webRoot, clientReleasesDir, release.name)); err != nil {
			return err
		}
	}
	return nil
}

/*
================
hashFile
================
*/
func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	length, err := io.Copy(digest, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), length, nil
}

/*
================
readReleaseSlice
================
*/
func readReleaseSlice(packageDir, file string, offset, length int64) ([]byte, error) {
	source, err := os.Open(filepath.Join(packageDir, filepath.FromSlash(file)))
	if err != nil {
		return nil, err
	}
	defer source.Close()
	slice := make([]byte, length)
	if _, err := source.ReadAt(slice, offset); err != nil && !(errors.Is(err, io.EOF) && length == 0) {
		return nil, fmt.Errorf("read %s[%d:%d]: %w", file, offset, offset+length, err)
	}
	return slice, nil
}

/*
================
writeReleaseFile
================
*/
func writeReleaseFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), clientReleaseDirMode); err != nil {
		return err
	}
	return os.WriteFile(path, data, clientReleaseFileMode)
}
