/*
===========================================================================

authority_upgrade.go - the offline, preserving authority upgrade

Brings every schema from UpgradeFromVersion up to CurrentVersion and
CurrentLayoutVersion. Takes the same exclusive authority lock as the game
server, validates every existing record and keeps an independent backup.
Two tables drive it: storeSteps (schema.go) says which layouts a store
at each schema may hold, and layoutAdditions which empty tables each
later layout added. Existing tables and their records survive unchanged; the new record
fields are optional. The role repair makes each guild master the sole
commander (guild_leader_role.go); it also runs on a current store that
needs it.
This operation is never called by server startup or a network request.

===========================================================================
*/
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// UpgradeFromVersion is the oldest schema the offline upgrade converts.
// Release admission treats it as an inclusive lower bound, not one version.
const UpgradeFromVersion = 13

const preMallLayoutVersion = 4
const preFortressLayoutVersion = 5
const preGuildWarLayoutVersion = 6
const preItemForgeLayoutVersion = 7
const preCompanionVersion = 14
const preWorldPointVersion = 15
const preTradeRewardVersion = 16
const preEndedQuestVersion = 17
const preJobRewardVersion = 18
const preStallDecorationVersion = 19
const preItemForgeVersion = 20
const preCommunityVersion = 21
const preUniqueKillLayoutVersion = 8
const preGearPrivacyVersion = 22
const prePetTrainingVersion = 23
const preSequentialPetTreeVersion = 24
const preMostWantedVersion = 23
const preLedgerVersion = 24
const preLedgerLayoutVersion = 9
const preGuildFoundedVersion = 25
const preAcademyHonorVersion = 26
const preMergedPetVersion = 27
const preInstanceReturnVersion = 28
const preStructureMadeVersion = 29
const preStoredPetCastsVersion = 30
const preBreakNoticeVersion = 31
const preRenameVersion = 32
const preHalloweenVersion = 33
const preMostWantedExpiryVersion = 34
const preHalloweenAccountVersion = 35
const preOnlineRecordVersion = 36
const preAcademyBuffVersion = 37
const preSilkHistoryVersion = 38
const preWantedDecayVersion = 39
const preGourdCountVersion = 40
const preArenaChestVersion = 41
const preSookArenaAccountVersion = 42
const preNameReservationLayoutVersion = 10
const preSilkHistoryLayoutVersion = 11

/*
================
upgradeSourceLayout

The layout the source store is validated at: its own, once its schema is
one the upgrade converts and the layout lies inside that schema's window.
================
*/
func upgradeSourceLayout(schema, layout int) (int, error) {
	step, ok := storeSteps[schema]
	if !ok {
		return 0, fmt.Errorf("authority upgrade: unsupported source schema %d", schema)
	}
	if layout < step.minLayout || layout > step.maxLayout {
		return 0, fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
	}
	return layout, nil
}

/*
================
layoutAddition

The empty tables one layout added, and the statement that creates them.
A source below the layout gains them; ascending order is creation order.
================
*/
type layoutAddition struct {
	layout int
	tables []string
	schema string
}

var layoutAdditions = []layoutAddition{
	{5, []string{"mall_accounts", "account_storage"}, mallAccountsSchema + accountStorageSchema},
	{6, []string{"fortresses", "fortress_requests", "fortress_structures", "alliances"}, fortressSchema + allianceSchema},
	{7, []string{"guild_wars"}, guildWarSchema},
	{8, []string{"fortress_item_forges"}, fortressItemForgeSchema},
	{9, []string{"unique_kills"}, uniqueKillsSchema},
	{10, []string{"economy_ledger"}, economyLedgerSchema},
	{11, []string{"name_reservations"}, nameReservationsSchema},
	{12, []string{"silk_history"}, silkHistorySchema},
}

// ErrAuthorityCurrent reports an authority already in the current format: a
// release retried after a committed upgrade has nothing left to do.
var ErrAuthorityCurrent = errors.New("authority upgrade: already in the current format")

/*
================
UpgradeAuthority

Without commit this validates the source and its exclusive ownership only.
With commit it returns the retained backup path, including on a later failure.
Account currencies start absent (zero); no character or inventory is rewritten.
An authority already in the current format answers ErrAuthorityCurrent after
the same validation, never a second upgrade.
================
*/
func UpgradeAuthority(dir string, commit bool) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, DBFileName)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("authority upgrade: source is not a regular database")
	}
	release, _, err := claimAuthority(dir, time.Now())
	if err != nil {
		return "", err
	}
	defer release()
	db, err := connectDB(path)
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := quickCheck(context.Background(), db); err != nil {
		return "", err
	}
	schema, err := readMetaInt(db, metaKeySchemaVersion, 0)
	if err != nil {
		return "", err
	}
	layout, err := readMetaInt(db, metaKeyLayoutVersion, 0)
	if err != nil {
		return "", err
	}
	repairs, err := leaderRoleRepairs(db)
	if err != nil {
		return "", fmt.Errorf("authority upgrade: master roles: %w", err)
	}
	if schema == CurrentVersion && layout == CurrentLayoutVersion && len(repairs) == 0 {
		if _, err := loadDB(db, CurrentVersion, CurrentLayoutVersion, 0); err != nil {
			return "", fmt.Errorf("authority upgrade: current authority validation: %w", err)
		}
		return "", ErrAuthorityCurrent
	}
	sourceLayout, err := upgradeSourceLayout(schema, layout)
	if err != nil {
		return "", err
	}
	if _, err := loadDB(db, schema, sourceLayout, 0); err != nil {
		return "", fmt.Errorf("authority upgrade: source validation: %w", err)
	}
	var additions []layoutAddition
	for _, addition := range layoutAdditions {
		if sourceLayout < addition.layout {
			additions = append(additions, addition)
		}
	}
	for _, addition := range additions {
		for _, table := range addition.tables {
			var existing int
			if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = ?", table).Scan(&existing); err != nil {
				return "", err
			}
			if existing == 0 {
				continue
			}
			// An empty table in an older-layout authority is construction
			// residue (a database created by a newer binary, then stamped
			// down); the CREATE below is a no-op for it. Rows are the
			// mislabeling this guard exists to catch: an old layout cannot
			// hold data its binary never wrote.
			var rows int
			if err := db.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&rows); err != nil {
				return "", err
			}
			if rows != 0 {
				return "", fmt.Errorf("authority upgrade: layout %d unexpectedly contains %s", sourceLayout, table)
			}
		}
	}
	if !commit {
		return "", nil
	}
	// A unique, restrictive file prevents replacing any prior recovery copy.
	backup, err := os.CreateTemp(dir, "state.before-upgrade-*.db")
	if err != nil {
		return "", err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		return backupPath, err
	}
	if _, err := db.Exec("VACUUM INTO ?", backupPath); err != nil {
		return backupPath, fmt.Errorf("authority upgrade: backup: %w", err)
	}
	file, err := os.OpenFile(backupPath, os.O_RDWR, 0)
	if err != nil {
		return backupPath, err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return backupPath, syncErr
	}
	if closeErr != nil {
		return backupPath, closeErr
	}
	if err := syncDir(dir); err != nil {
		return backupPath, err
	}
	if _, err := db.Exec("PRAGMA synchronous=FULL"); err != nil {
		return backupPath, err
	}
	tx, err := db.Begin()
	if err != nil {
		return backupPath, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, addition := range additions {
		if _, err := tx.Exec(addition.schema); err != nil {
			return backupPath, err
		}
	}
	if err := applyLeaderRoleRepairs(tx, repairs); err != nil {
		return backupPath, err
	}
	if err := stampVersionTx(tx, metaKeyLayoutVersion, CurrentLayoutVersion); err != nil {
		return backupPath, err
	}
	if err := stampVersionTx(tx, metaKeySchemaVersion, CurrentVersion); err != nil {
		return backupPath, err
	}
	if err := tx.Commit(); err != nil {
		return backupPath, err
	}
	if _, err := loadDB(db, CurrentVersion, CurrentLayoutVersion, 0); err != nil {
		return backupPath, fmt.Errorf("authority upgrade committed, validation failed; retain backup %s: %w", backupPath, err)
	}
	return backupPath, nil
}
