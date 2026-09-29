// Package gamedata validates the immutable, generated data projection used by
// GameWorld. It deliberately knows nothing about source checkouts or browser
// publish directories: build tooling may create a bundle from lawful source
// material, but runtime code consumes only this projection contract.
package gamedata

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ManifestFilename         = "manifest.json"
	ManifestFormat           = "sro-game-data-bundle"
	SupportedSchemaVersion   = 1
	SupportedGameVersion     = "1.150"
	SupportedProjection      = "server"
	SupportedProtocolVersion = 2

	maxManifestBytes = 16 << 20
	maxBundleFiles   = 100_000
	maxBundleBytes   = int64(32) << 30
)

var requiredServerFiles = []string{
	"character-authority/catalog.json",
	"textdata/characterdata.txt",
	"textdata/gachaitemset.txt",
	"textdata/gachanpcmap.txt",
	"textdata/itemdata.txt",
	"textdata/leveldata.txt",
	"textdata/magicoption.txt",
	"textdata/npcpos.txt",
	"textdata/questdata.txt",
	"textdata/skilldata.txt",
	"world-authority/areas/catalog.json",
	"world-authority/movement/catalog.json",
}

// Descriptor identifies one exact file in a projection. Its shape follows
// the OCI descriptor properties that matter for a filesystem bundle: media
// type, content digest, and byte size.
type Descriptor struct {
	Path      string `json:"path"`
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

// Manifest is the version and compatibility boundary for one projection.
// ContentDigest commits to the ordered descriptor set; ManifestDigest (on
// Bundle) commits to the exact manifest bytes and is what deployment pins.
type Manifest struct {
	Format          string       `json:"format"`
	SchemaVersion   int          `json:"schemaVersion"`
	GameVersion     string       `json:"gameVersion"`
	DataVersion     string       `json:"dataVersion"`
	SourceRevision  string       `json:"sourceRevision"`
	Projection      string       `json:"projection"`
	ProtocolVersion int          `json:"protocolVersion"`
	ContentDigest   string       `json:"contentDigest"`
	Files           []Descriptor `json:"files"`
}

// Bundle is a fully verified server projection. Callers receive only paths
// inside the verified root; they never reconstruct sibling-workspace paths.
type Bundle struct {
	Root                  string
	Manifest              Manifest
	ManifestDigest        string
	CharacterAuthorityDir string
	TextdataDir           string
	WorldAuthorityDir     string
}

// Open validates identity, compatibility, the exact file set, every file
// size/digest, and the projection tree digest. expectedManifestDigest may be
// empty for a local inspection; deployments pass a sha256 digest pinned by
// their desired state.
func Open(root, expectedManifestDigest string) (Bundle, error) {
	absoluteRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return Bundle{}, fmt.Errorf("resolve root: %w", err)
	}
	rootInfo, err := os.Stat(absoluteRoot)
	if err != nil {
		return Bundle{}, fmt.Errorf("root %q: %w", absoluteRoot, err)
	}
	if !rootInfo.IsDir() {
		return Bundle{}, fmt.Errorf("root %q is not a directory", absoluteRoot)
	}
	realRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return Bundle{}, fmt.Errorf("resolve root links: %w", err)
	}

	// Every read goes through one os.Root: the OS refuses a path that
	// escapes it, symlinks included, so no read resolves links itself.
	tree, err := os.OpenRoot(realRoot)
	if err != nil {
		return Bundle{}, fmt.Errorf("open root: %w", err)
	}
	defer func() { _ = tree.Close() }()

	manifestBytes, err := readBoundedRegularFile(tree, ManifestFilename, maxManifestBytes)
	if err != nil {
		return Bundle{}, fmt.Errorf("manifest: %w", err)
	}
	manifestDigest := digestBytes(manifestBytes)
	if expected := strings.TrimSpace(expectedManifestDigest); expected != "" {
		if err := validateSHA256Digest(expected); err != nil {
			return Bundle{}, fmt.Errorf("expected manifest digest: %w", err)
		}
		if manifestDigest != expected {
			return Bundle{}, fmt.Errorf("manifest digest %s does not match deployment pin %s", manifestDigest, expected)
		}
	}

	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Bundle{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Bundle{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return Bundle{}, err
	}

	described := make(map[string]Descriptor, len(manifest.Files))
	for _, descriptor := range manifest.Files {
		described[descriptor.Path] = descriptor
		actualDigest, actualSize, err := digestRegularFile(tree, filepath.FromSlash(descriptor.Path), descriptor.Size)
		if err != nil {
			return Bundle{}, fmt.Errorf("file %s: %w", descriptor.Path, err)
		}
		if actualSize != descriptor.Size {
			return Bundle{}, fmt.Errorf("file %s size %d does not match manifest %d", descriptor.Path, actualSize, descriptor.Size)
		}
		if actualDigest != descriptor.Digest {
			return Bundle{}, fmt.Errorf("file %s digest %s does not match manifest %s", descriptor.Path, actualDigest, descriptor.Digest)
		}
	}
	if err := validateExactFileSet(tree, described); err != nil {
		return Bundle{}, err
	}

	return Bundle{
		Root:                  filepath.Clean(realRoot),
		Manifest:              manifest,
		ManifestDigest:        manifestDigest,
		CharacterAuthorityDir: filepath.Join(realRoot, "character-authority"),
		TextdataDir:           filepath.Join(realRoot, "textdata"),
		WorldAuthorityDir:     filepath.Join(realRoot, "world-authority"),
	}, nil
}

func validateManifest(manifest Manifest) error {
	if manifest.Format != ManifestFormat {
		return fmt.Errorf("unsupported manifest format %q", manifest.Format)
	}
	if manifest.SchemaVersion != SupportedSchemaVersion {
		return fmt.Errorf("unsupported game-data schema %d (supported: %d)", manifest.SchemaVersion, SupportedSchemaVersion)
	}
	if manifest.GameVersion != SupportedGameVersion {
		return fmt.Errorf("game-data version %q is incompatible with GameWorld %s", manifest.GameVersion, SupportedGameVersion)
	}
	if manifest.Projection != SupportedProjection {
		return fmt.Errorf("projection %q is not the required %q projection", manifest.Projection, SupportedProjection)
	}
	if manifest.ProtocolVersion != SupportedProtocolVersion {
		return fmt.Errorf("game-data protocol %d is incompatible with GameWorld protocol %d", manifest.ProtocolVersion, SupportedProtocolVersion)
	}
	if err := validateLabel("dataVersion", manifest.DataVersion); err != nil {
		return err
	}
	if err := validateLabel("sourceRevision", manifest.SourceRevision); err != nil {
		return err
	}
	if len(manifest.Files) == 0 || len(manifest.Files) > maxBundleFiles {
		return fmt.Errorf("manifest has %d files; expected 1..%d", len(manifest.Files), maxBundleFiles)
	}
	if err := validateSHA256Digest(manifest.ContentDigest); err != nil {
		return fmt.Errorf("contentDigest: %w", err)
	}

	seenFolded := make(map[string]struct{}, len(manifest.Files))
	seenRequired := make(map[string]bool, len(requiredServerFiles))
	var total int64
	previous := ""
	for index, descriptor := range manifest.Files {
		if !fs.ValidPath(descriptor.Path) || descriptor.Path == ManifestFilename || path.Clean(descriptor.Path) != descriptor.Path {
			return fmt.Errorf("files[%d].path %q is not a safe bundle-relative path", index, descriptor.Path)
		}
		if previous != "" && descriptor.Path <= previous {
			return fmt.Errorf("file descriptors are not strictly path-sorted at %q", descriptor.Path)
		}
		previous = descriptor.Path
		folded := strings.ToLower(descriptor.Path)
		if _, duplicate := seenFolded[folded]; duplicate {
			return fmt.Errorf("case-insensitive duplicate file path %q", descriptor.Path)
		}
		seenFolded[folded] = struct{}{}
		if descriptor.MediaType == "" || len(descriptor.MediaType) > 128 || strings.TrimSpace(descriptor.MediaType) != descriptor.MediaType {
			return fmt.Errorf("files[%d].mediaType is invalid", index)
		}
		if descriptor.Size < 0 || descriptor.Size > maxBundleBytes-total {
			return fmt.Errorf("files[%d].size exceeds the %d-byte bundle limit", index, maxBundleBytes)
		}
		total += descriptor.Size
		if err := validateSHA256Digest(descriptor.Digest); err != nil {
			return fmt.Errorf("files[%d].digest: %w", index, err)
		}
		seenRequired[descriptor.Path] = true
	}
	for _, required := range requiredServerFiles {
		if !seenRequired[required] {
			return fmt.Errorf("required server projection file %s is absent", required)
		}
	}
	if digest := ContentDigest(manifest.Files); digest != manifest.ContentDigest {
		return fmt.Errorf("contentDigest %s does not match descriptor tree %s", manifest.ContentDigest, digest)
	}
	return nil
}

func validateLabel(name, value string) error {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s is invalid", name)
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return fmt.Errorf("%s must contain printable ASCII without spaces", name)
		}
	}
	return nil
}

func validateSHA256Digest(value string) error {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+sha256.Size*2 {
		return fmt.Errorf("%q is not a sha256 digest", value)
	}
	encoded := value[len(prefix):]
	if encoded != strings.ToLower(encoded) {
		return fmt.Errorf("sha256 digest must use lowercase hexadecimal")
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return fmt.Errorf("invalid sha256 hexadecimal: %w", err)
	}
	return nil
}

// ContentDigest returns the deterministic digest of an already path-sorted
// descriptor set. Length-prefixed fields avoid depending on JSON map order or
// a language-specific canonical JSON implementation.
func ContentDigest(files []Descriptor) string {
	digest := sha256.New()
	var length [8]byte
	for _, descriptor := range files {
		writeFramedString(digest, length[:], descriptor.Path)
		writeFramedString(digest, length[:], descriptor.MediaType)
		binary.BigEndian.PutUint64(length[:], uint64(descriptor.Size))
		_, _ = digest.Write(length[:])
		writeFramedString(digest, length[:], descriptor.Digest)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func writeFramedString(writer io.Writer, scratch []byte, value string) {
	binary.BigEndian.PutUint64(scratch, uint64(len(value)))
	_, _ = writer.Write(scratch)
	_, _ = io.WriteString(writer, value)
}

func digestBytes(contents []byte) string {
	sum := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestRegularFile(root *os.Root, filename string, expectedSize int64) (string, int64, error) {
	file, err := openVerifiedRegularFile(root, filename)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if info.Size() != expectedSize {
		return "", info.Size(), nil
	}
	digest := sha256.New()
	read, err := io.Copy(digest, bufio.NewReader(file))
	if err != nil {
		return "", read, err
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), read, nil
}

func readBoundedRegularFile(root *os.Root, filename string, limit int64) ([]byte, error) {
	file, err := openVerifiedRegularFile(root, filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < 0 || info.Size() > limit {
		return nil, fmt.Errorf("size %d exceeds limit %d", info.Size(), limit)
	}
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("file grew beyond limit %d while reading", limit)
	}
	return contents, nil
}

// openVerifiedRegularFile opens a root-relative regular file. os.Root
// rejects any path, symlinked or not, that resolves outside the root.
func openVerifiedRegularFile(root *os.Root, filename string) (*os.File, error) {
	file, err := root.Open(filename)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("%s is not a regular file", filename)
	}
	return file, nil
}

// validateExactFileSet walks the root and requires exactly the described
// files: no symlink, no other file type, nothing undescribed.
func validateExactFileSet(root *os.Root, described map[string]Descriptor) error {
	actual := make([]string, 0, len(described))
	err := fs.WalkDir(root.FS(), ".", func(relative string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if relative == "." {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("bundle contains symlink %q", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("bundle contains non-regular file %q", relative)
		}
		if relative != ManifestFilename {
			actual = append(actual, relative)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk bundle: %w", err)
	}
	sort.Strings(actual)
	for _, relative := range actual {
		if _, ok := described[relative]; !ok {
			return fmt.Errorf("bundle contains undescribed file %s", relative)
		}
	}
	if len(actual) != len(described) {
		return fmt.Errorf("bundle contains %d data files but manifest describes %d", len(actual), len(described))
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("multiple JSON values are not allowed")
}
