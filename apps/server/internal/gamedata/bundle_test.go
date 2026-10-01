package gamedata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestOpenValidatesCompletePinnedBundle(t *testing.T) {
	licensed.RequireGameData(t)
	root, manifestDigest := writeTestBundle(t, nil)

	bundle, err := Open(root, manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest.GameVersion != SupportedGameVersion ||
		bundle.Manifest.ProtocolVersion != SupportedProtocolVersion {
		t.Fatalf("unexpected manifest: %+v", bundle.Manifest)
	}
	if bundle.CharacterAuthorityDir != filepath.Join(root, "character-authority") ||
		bundle.TextdataDir != filepath.Join(root, "textdata") ||
		bundle.WorldAuthorityDir != filepath.Join(root, "world-authority") {
		t.Fatalf("unexpected projection paths: %+v", bundle)
	}
}

func TestOpenRefusesTamperedFile(t *testing.T) {
	root, _ := writeTestBundle(t, nil)
	path := filepath.Join(root, "textdata", "skilldata.txt")
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Open(root, "")
	if err == nil || !strings.Contains(err.Error(), "skilldata.txt") {
		t.Fatalf("tamper error = %v", err)
	}
}

func TestOpenRefusesUndescribedFile(t *testing.T) {
	root, _ := writeTestBundle(t, nil)
	if err := os.WriteFile(filepath.Join(root, "surprise.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Open(root, "")
	if err == nil || !strings.Contains(err.Error(), "undescribed file") {
		t.Fatalf("extra-file error = %v", err)
	}
}

func TestOpenRefusesWrongDeploymentPin(t *testing.T) {
	root, _ := writeTestBundle(t, nil)
	wrong := "sha256:" + strings.Repeat("0", 64)

	_, err := Open(root, wrong)
	if err == nil || !strings.Contains(err.Error(), "deployment pin") {
		t.Fatalf("pin error = %v", err)
	}
}

func TestOpenRefusesIncompatibleIdentity(t *testing.T) {
	root, _ := writeTestBundle(t, func(manifest *Manifest) {
		manifest.GameVersion = "1.188"
	})

	_, err := Open(root, "")
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("identity error = %v", err)
	}
}

func TestOpenRefusesUnsafeDescriptorPath(t *testing.T) {
	root, _ := writeTestBundle(t, func(manifest *Manifest) {
		manifest.Files[0].Path = "../escape.txt"
		manifest.ContentDigest = ContentDigest(manifest.Files)
	})

	_, err := Open(root, "")
	if err == nil || !strings.Contains(err.Error(), "safe bundle-relative") {
		t.Fatalf("path error = %v", err)
	}
}

/*
================
realTempDir

t.TempDir with symlinks resolved. Open and Resolve report the real root, and
macOS TMPDIR lives under /var, a symlink to /private/var.
================
*/
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeTestBundle(t *testing.T, mutate func(*Manifest)) (string, string) {
	t.Helper()
	root := realTempDir(t)
	paths := append([]string(nil), requiredServerFiles...)
	paths = append(paths, "textdata/characterdata_5000.txt")
	sort.Strings(paths)
	files := make([]Descriptor, 0, len(paths))
	for _, relative := range paths {
		contents := []byte("fixture:" + relative + "\n")
		filename := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(contents)
		files = append(files, Descriptor{
			Path:      relative,
			MediaType: mediaTypeForTestPath(relative),
			Size:      int64(len(contents)),
			Digest:    "sha256:" + hex.EncodeToString(sum[:]),
		})
	}
	manifest := Manifest{
		Format:          ManifestFormat,
		SchemaVersion:   SupportedSchemaVersion,
		GameVersion:     SupportedGameVersion,
		DataVersion:     "test.1",
		SourceRevision:  "test-source",
		Projection:      SupportedProjection,
		ProtocolVersion: SupportedProtocolVersion,
		ContentDigest:   ContentDigest(files),
		Files:           files,
	}
	if mutate != nil {
		mutate(&manifest)
	}
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(filepath.Join(root, ManifestFilename), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	return root, "sha256:" + hex.EncodeToString(sum[:])
}

func mediaTypeForTestPath(path string) string {
	if strings.HasSuffix(path, ".json") {
		return "application/json"
	}
	return "text/plain"
}
