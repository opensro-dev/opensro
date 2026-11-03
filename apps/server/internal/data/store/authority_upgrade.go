/*
===========================================================================

authority_upgrade.go - the offline, preserving authority upgrade

Brings schemas 13 through 33 to schema 34, layout 11. Takes the same exclusive authority lock as the game
server, validates every existing record and keeps an independent backup.
Schema 13 also gains the two account tables from layout 5; every layout 5
source gains the empty fortress tables of layout 6, and every layout 7
source the empty fortress production table of layout 8, every layout 8
source the empty unique-kill table of layout 9, and every layout 10
source the empty name-reservation table of layout 11. Existing tables and
their records survive unchanged; the new record fields are optional. The
role repair makes each guild master the sole commander
(guild_leader_role.go); it also runs on a current store that needs it.
This operation is never called by server startup or a network request.

===========================================================================
*/
package store

import (
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
const preNameReservationLayoutVersion = 10

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
	if err := quickCheck(db); err != nil {
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
		if _, err := loadDB(db, CurrentVersion, CurrentLayoutVersion); err != nil {
			return "", fmt.Errorf("authority upgrade: current authority validation: %w", err)
		}
		return "", ErrAuthorityCurrent
	}
	sourceLayout := preFortressLayoutVersion
	switch schema {
	case UpgradeFromVersion:
		sourceLayout = preMallLayoutVersion
	case preCompanionVersion, preWorldPointVersion:
	case preTradeRewardVersion:
		if layout >= preGuildWarLayoutVersion && layout <= preItemForgeLayoutVersion {
			sourceLayout = layout
		}
	case preEndedQuestVersion:
		// Schema 17 ran at layouts 5 through 7. Its records may already carry
		// endedQuestIds (the schema 17 server 491cc962 wrote them), which the
		// current decoder reads; nothing is rewritten.
		if layout < preFortressLayoutVersion || layout > preItemForgeLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preJobRewardVersion:
		// Schema 18 ran at layouts 5 through 7 and has no job reward or
		// ranking snapshot; its records read unchanged.
		if layout < preFortressLayoutVersion || layout > preItemForgeLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preStallDecorationVersion:
		// Schema 19 ran at layouts 5 through 7 and has no stall decoration;
		// its records read unchanged.
		if layout < preFortressLayoutVersion || layout > preItemForgeLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preItemForgeVersion:
		// Schema 20 ran at layouts 5 through 7 and has no production table;
		// its records read unchanged.
		if layout < preFortressLayoutVersion || layout > preItemForgeLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preCommunityVersion:
		// Schema 21 ran at layouts 5 through 8 and has no site fields or
		// unique-kill table; its records read unchanged.
		if layout < preFortressLayoutVersion || layout > preUniqueKillLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preGearPrivacyVersion:
		// Schema 22 ran at layouts 5 through 9 and has no gear privacy field;
		// its records read unchanged.
		if layout < preFortressLayoutVersion || layout > preLedgerLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preMostWantedVersion:
		// Schema 23 ran at layouts 5 through 9 and has no most-wanted streak;
		// its records read unchanged.
		if layout < preFortressLayoutVersion || layout > preLedgerLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preLedgerVersion:
		// Schema 24 ran at layouts 5 through 9 and has no economy ledger;
		// its records read unchanged.
		if layout < preFortressLayoutVersion || layout > preLedgerLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preGuildFoundedVersion:
		// Schema 25 ran at layouts 5 through 10 and has no guild founding
		// time; its records read unchanged.
		if layout < preFortressLayoutVersion || layout > CurrentLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preStoredPetCastsVersion, preBreakNoticeVersion:
		// Stored-cast fields and the break-notice opt-in are optional;
		// old records read unchanged. These schemas ran at layout 10.
		if layout < preNameReservationLayoutVersion || layout > CurrentLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preRenameVersion:
		// Schema 32 ran at layout 10 and has no name-change scroll state
		// or reservation table; its records read unchanged.
		if layout < preNameReservationLayoutVersion || layout > CurrentLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preHalloweenVersion:
		// Schema 33 ran at layout 11 and has no Halloween box count; its
		// records read unchanged.
		if layout < preNameReservationLayoutVersion || layout > CurrentLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preAcademyHonorVersion, preMergedPetVersion:
		// Schema 26 ran at layouts 5 through 10 and has no training-camp
		// honor/evaluation fields or disband timestamp; its records read
		// unchanged.
		if layout < preFortressLayoutVersion || layout > CurrentLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preInstanceReturnVersion:
		// Schema 28 ran at layouts 5 through 10 and has no instance return
		// anchor; its records read unchanged.
		if layout < preFortressLayoutVersion || layout > CurrentLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case preStructureMadeVersion:
		// Schema 29 ran at layouts 5 through 10 and has no structure made
		// time; its records read unchanged.
		if layout < preFortressLayoutVersion || layout > CurrentLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	case CurrentVersion:
		// A current store still upgrades when commander roles disagree with
		// member grades (guild_leader_role.go): the repair is its only change.
		if layout < preFortressLayoutVersion || layout > CurrentLayoutVersion {
			return "", fmt.Errorf("authority upgrade: unsupported layout %d for schema %d", layout, schema)
		}
		sourceLayout = layout
	default:
		return "", fmt.Errorf("authority upgrade: unsupported source schema %d", schema)
	}
	if _, err := loadDB(db, schema, sourceLayout); err != nil {
		return "", fmt.Errorf("authority upgrade: source validation: %w", err)
	}
	var added []string
	if sourceLayout < preGuildWarLayoutVersion {
		added = []string{"fortresses", "fortress_requests", "fortress_structures", "alliances"}
	}
	if sourceLayout < preItemForgeLayoutVersion {
		added = append(added, "guild_wars")
	}
	if sourceLayout < preUniqueKillLayoutVersion {
		added = append(added, "fortress_item_forges")
	}
	if sourceLayout < preLedgerLayoutVersion {
		added = append(added, "unique_kills")
	}
	if sourceLayout <= preLedgerLayoutVersion {
		added = append(added, "economy_ledger")
	}
	if sourceLayout <= preNameReservationLayoutVersion {
		added = append(added, "name_reservations")
	}
	if sourceLayout == preMallLayoutVersion {
		added = append(added, "mall_accounts", "account_storage")
	}
	for _, table := range added {
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
	if sourceLayout == preMallLayoutVersion {
		if _, err := tx.Exec(mallAccountsSchema + accountStorageSchema); err != nil {
			return backupPath, err
		}
	}
	if sourceLayout < preGuildWarLayoutVersion {
		if _, err := tx.Exec(fortressSchema + allianceSchema); err != nil {
			return backupPath, err
		}
	}
	if sourceLayout < preItemForgeLayoutVersion {
		if _, err := tx.Exec(guildWarSchema); err != nil {
			return backupPath, err
		}
	}
	if sourceLayout < preUniqueKillLayoutVersion {
		if _, err := tx.Exec(fortressItemForgeSchema); err != nil {
			return backupPath, err
		}
	}
	if sourceLayout < preLedgerLayoutVersion {
		if _, err := tx.Exec(uniqueKillsSchema); err != nil {
			return backupPath, err
		}
	}
	if sourceLayout <= preLedgerLayoutVersion {
		if _, err := tx.Exec(economyLedgerSchema); err != nil {
			return backupPath, err
		}
	}
	if sourceLayout <= preNameReservationLayoutVersion {
		if _, err := tx.Exec(nameReservationsSchema); err != nil {
			return backupPath, err
		}
	}
	if err := applyLeaderRoleRepairs(tx, repairs); err != nil {
		return backupPath, err
	}
	if err := upsertMetaTx(tx, metaKeyLayoutVersion, fmt.Sprint(CurrentLayoutVersion)); err != nil {
		return backupPath, err
	}
	if err := upsertMetaTx(tx, metaKeySchemaVersion, fmt.Sprint(CurrentVersion)); err != nil {
		return backupPath, err
	}
	if err := tx.Commit(); err != nil {
		return backupPath, err
	}
	if _, err := loadDB(db, CurrentVersion, CurrentLayoutVersion); err != nil {
		return backupPath, fmt.Errorf("authority upgrade committed, validation failed; retain backup %s: %w", backupPath, err)
	}
	return backupPath, nil
}
