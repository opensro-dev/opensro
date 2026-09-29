/*
===========================================================================

shippedtextdata_shared_test.go - shared shipped textdata for this package's tests

===========================================================================
*/

package enterworld

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"path/filepath"
	"sync"
	"testing"
)

/*
==================
realAssetPaths

The development paths over the verified projection, resolved inside the
test so Go's test cache sees the data it reads.
==================
*/
func realAssetPaths(t testing.TB) DevPaths {
	t.Helper()
	root := gamedatatest.Paths(t).BundleRoot
	return DevPaths{
		RosterPath:        filepath.Join(root, "character-authority", "catalog.json"),
		TextdataDir:       filepath.Join(root, "textdata"),
		MissionChatPath:   filepath.Join("..", "..", "..", "config", "mission-chat.json"),
		EquipItemsEnabled: true,
	}
}

// The shipped textdata parses dominate this package's test cost (itemdata
// ~12.6MB + textdataname ~4.2MB, skilldata ~21MB), so the read-only
// shipped-data tests share ONE loader each instead of re-parsing per
// test. The loaders' lazy sync.Once makes a shared instance safe for
// concurrent readers, so this composes with t.Parallel(). TEST-ONLY:
// production wiring (devdeps.go) still builds its own loaders, and a test
// that asserts construction or degradation behavior must keep building
// its own too.
var (
	sharedShippedItemsOnce        sync.Once
	sharedShippedItemsInst        *TextdataItems
	sharedShippedSkillsOnce       sync.Once
	sharedShippedSkillsInst       *TextdataSkills
	sharedShippedMagicOptionsOnce sync.Once
	sharedShippedMagicOptionsInst *TextdataMagicOptions
)

/*
==================
sharedShippedItems

sharedShippedItems returns the package-wide itemdata loader over the
extracted textdata current-contract tests read (realAssetPaths), skipping
when this checkout has no media.
==================
*/
func sharedShippedItems(t *testing.T) *TextdataItems {
	t.Helper()
	dir := realAssetPaths(t).TextdataDir
	sharedShippedItemsOnce.Do(func() {
		sharedShippedItemsInst = NewTextdataItems(dir)
	})
	return sharedShippedItemsInst
}

/*
==================
sharedShippedMagicOptions

sharedShippedMagicOptions returns the package-wide magicoption.txt loader
over the extracted textdata (realAssetPaths), skipping when this checkout
has no media.
==================
*/
func sharedShippedMagicOptions(t *testing.T) *TextdataMagicOptions {
	t.Helper()
	dir := realAssetPaths(t).TextdataDir
	sharedShippedMagicOptionsOnce.Do(func() {
		sharedShippedMagicOptionsInst = NewTextdataMagicOptions(dir)
	})
	return sharedShippedMagicOptionsInst
}

/*
==================
sharedShippedSkills

sharedShippedSkills returns the package-wide skilldata loader from the
same process-level path contract used by GameWorld.
The projection resolves once per process: callers read it inside per-row
loops, and each resolution re-identifies the artifact on disk.
==================
*/
func sharedShippedSkills(t *testing.T) *TextdataSkills {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	sharedShippedSkillsOnce.Do(func() {
		sharedShippedSkillsInst = NewTextdataSkills(dir)
	})
	return sharedShippedSkillsInst
}
