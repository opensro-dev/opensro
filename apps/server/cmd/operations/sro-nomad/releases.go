/*
===========================================================================

releases.go - immutable service releases

Publishes verified executables and retains releases referenced by Nomad job
history so native rollback can find its artifacts.

===========================================================================
*/
package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Release artifacts contain no secrets. A separate task identity needs read/execute, never write.
const releaseAccessMode = 0o755

/*
================
releaseID
================
*/
func releaseID(paths ...string) (string, error) {
	digest := sha256.New()
	for _, path := range paths {
		sum, err := fileSHA256(path)
		if err != nil {
			return "", err
		}
		_, _ = digest.Write(sum)
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

/*
================
fileSHA256
================
*/
func fileSHA256(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	_, copyErr := io.Copy(digest, file)
	closeErr := file.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return digest.Sum(nil), nil
}

/*
================
stageReleases
================
*/
func (deployment *deployment) stageReleases() error {
	for _, release := range []struct {
		source      string
		destination string
		template    string
		identity    string
	}{
		{
			deployment.AgentSource,
			deployment.AgentBinary,
			filepath.Join(deployment.JobsDir, agentTemplateName),
			deployment.AgentReleaseID,
		},
		{
			deployment.GameSource,
			deployment.GameBinary,
			filepath.Join(deployment.JobsDir, gameTemplateName),
			deployment.GameReleaseID,
		},
	} {
		if err := stageRelease(release.source, release.destination); err != nil {
			return err
		}
		stagedIdentity, err := releaseID(
			release.destination,
			release.template,
		)
		if err != nil {
			return fmt.Errorf(
				"verify staged release %s: %w",
				release.destination,
				err,
			)
		}
		if stagedIdentity != release.identity {
			return fmt.Errorf(
				"release inputs changed while staging %s",
				release.destination,
			)
		}
	}
	return nil
}

/*
================
stageRelease
================
*/
func stageRelease(source, destination string) error {
	sourceDigest, err := fileSHA256(source)
	if err != nil {
		return fmt.Errorf("hash release source %s: %w", source, err)
	}
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() ||
			info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf(
				"immutable release %s is not a regular file",
				destination,
			)
		}
		destinationDigest, err := fileSHA256(destination)
		if err != nil {
			return err
		}
		if !bytes.Equal(sourceDigest, destinationDigest) {
			return fmt.Errorf(
				"immutable release %s differs from source %s",
				destination,
				source,
			)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, releaseAccessMode); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(
			"release directory %s is not a real directory",
			directory,
		)
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	temporary, err := os.CreateTemp(directory, ".release-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	published := false
	defer func() {
		temporary.Close()
		if !published {
			os.Remove(temporaryPath)
		}
	}()
	if _, err := io.Copy(temporary, sourceFile); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Chmod(releaseAccessMode); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	published = true
	return nil
}

/*
================
pruneReleases
================
*/
func (deployment *deployment) pruneReleases(
	retained map[string]map[string]struct{},
) error {
	for _, release := range []struct {
		role string
	}{
		{"agent"},
		{"gameworld"},
	} {
		root := filepath.Join(deployment.ReleaseDir, release.role)
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if _, keep := retained[release.role][entry.Name()]; keep {
				continue
			}
			path := filepath.Join(root, entry.Name())
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf(
					"refusing unexpected release-cache entry %s",
					path,
				)
			}
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	return nil
}
