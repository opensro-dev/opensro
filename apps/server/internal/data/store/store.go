package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

// Env configuration (documented in ops/docs/DEPLOYMENT.md).
const (
	// EnvStateDir overrides the authority directory.
	EnvStateDir = "SRO_AUTHORITY_STATE_DIR"
	// EnvRequireStore, when "1", makes a missing store a refusal instead
	// of an empty first boot. The presence expectation lives outside the
	// directory it guards, because no in-directory marker survives deletion
	// of that directory.
	EnvRequireStore = "SRO_AUTHORITY_REQUIRE"
)

// DirFromEnv returns the explicitly configured authority directory. There is
// no process-wide fallback: a multi-shard server must either name its owned
// shard through DirForShardFromEnv or receive an explicit deployment mount.
func DirFromEnv() string {
	return strings.TrimSpace(os.Getenv(EnvStateDir))
}

// DirForShardFromEnv resolves one GameWorld process's authority directory.
// The explicit environment override supports deployment-mounted volumes;
// otherwise each shard receives a different path by construction.
func DirForShardFromEnv(shardID string) string {
	if dir := DirFromEnv(); dir != "" {
		return dir
	}
	return filepath.Join(".state", "shards", shardID, "authority")
}

func resolveAuthorityDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = DirFromEnv()
	}
	if dir == "" {
		return "", fmt.Errorf(
			"authority directory is required: pass one explicitly or set %s",
			EnvStateDir,
		)
	}
	return dir, nil
}

// Options configures Open.
type Options struct {
	// RequireStore refuses a missing store (production posture; see
	// EnvRequireStore).
	RequireStore bool
	// Now injects the clock (tests); nil = time.Now.
	Now func() time.Time
	// DefaultSkills resolves the required racial base-attack creation set.
	// CreateCharacter refuses when the seeder is absent or cannot resolve a
	// complete set; current-schema records may never be skill-less.
	DefaultSkills SkillSeedFunc
	// DefaultQuests resolves the racial creation quest seed (the retail
	// _RefCharDefault_Quest mechanism - internal/game/quest/seed.go; production
	// wires quest.DefaultQuestSeeder over the loaded definitions).
	// It is optional because no active quest is valid character state. A
	// wired seeder that fails still refuses creation rather than installing a
	// partial set.
	DefaultQuests QuestSeedFunc
	// DefaultInventory grants the creation choice's starter items and the
	// starting gold (the retail _AddNewChar item insert), so the character
	// list shows a new character dressed before its first world entry. It is
	// optional: without it the first enter-world bootstrap grants them.
	DefaultInventory InventorySeedFunc
}

// InventorySeedFunc installs the starter inventory and gold on a character
// that has none. It resolves item references only; it cannot fail.
type InventorySeedFunc func(c *domain.Character)

// QuestSeedFunc answers the racial creation active-quest records (the
// retail _RefCharDefault_Quest shape, codename-resolved by internal/game/quest).
type QuestSeedFunc func(raceKey string) ([]domain.ActiveQuestRecord, error)

// OptionsFromEnv builds the production Options from the environment.
func OptionsFromEnv() Options {
	return Options{RequireStore: os.Getenv(EnvRequireStore) == "1"}
}

// Health is the store's observable write condition: runtime
// write failures fail open and LOUD; this is the loud part tests, probes
// and the acceptance suite read).
type Health struct {
	LastError     string
	FailingSince  time.Time
	FailedWrites  int
	LastCommitAt  time.Time
	LoadedFromBak bool
}

// MetaView is a copy of the counters for boot logs and tests.
type MetaView struct {
	GidCounter uint32
	NextCharID map[string]int64
}

// changeSet is the store-owned unit-of-work state. It has no lock of its own:
// callers hold Store.mu while marking or consuming it so one SQLite
// transaction can persist changes across character and social planes.
type changeSet struct {
	all            bool
	characters     map[*domain.Character]bool
	nextCharID     map[string]bool
	nextGuildID    map[string]bool
	mailboxes      map[mailboxKey]bool
	guilds         map[guildKey]bool
	camps          map[campKey]bool
	dissolvedGuild map[guildKey]bool
}

func newChangeSet() changeSet {
	return changeSet{
		characters:     map[*domain.Character]bool{},
		nextCharID:     map[string]bool{},
		nextGuildID:    map[string]bool{},
		mailboxes:      map[mailboxKey]bool{},
		guilds:         map[guildKey]bool{},
		camps:          map[campKey]bool{},
		dissolvedGuild: map[guildKey]bool{},
	}
}

func (changes changeSet) empty() bool {
	return !changes.all &&
		len(changes.characters) == 0 &&
		len(changes.nextCharID) == 0 &&
		len(changes.nextGuildID) == 0 &&
		len(changes.mailboxes) == 0 &&
		len(changes.guilds) == 0 &&
		len(changes.camps) == 0 &&
		len(changes.dissolvedGuild) == 0
}

// groundState owns the boundary between the live gameplay registry and its
// last durable snapshot. The registry retains entity lifecycle; Store owns
// revision observation and persistence.
type groundState struct {
	source           GroundSnapshotSource
	loadedRecords    map[string][]domain.GroundItemRecord
	loadedGidCounter uint32
	committedRev     uint64
	revisionRecorded bool
}

// Store is the single authority for persisted gameplay state. One
// instance per process; guarded by the authority.lock liveness file.
//
// In-memory state is the runtime authority (the pointer-identity
// contract); the database is its crash-consistent shadow, advanced one
// transaction per committed operation.
type Store struct {
	mu sync.RWMutex

	dir       string
	dbPath    string
	dbBakPath string

	db *sql.DB

	characters       map[string][]*domain.Character
	characterLookups map[string]*characterLookupIndex
	deleted          map[string][]json.RawMessage
	meta             Meta

	// mailboxes is the letter-mailbox plane (the memos table): division
	// -> character id -> the ordered letter list. Slice order is the
	// wire order (domain.LetterStore contract); mutation is
	// copy-then-swap through MutateMailbox only.
	mailboxes map[string]map[int64][]domain.LetterRecord

	// guilds / guildMembers are the guild plane (the guilds and
	// guild_members tables): division -> guild id -> the guild row and
	// its ordered member list. Member slice order is the wire order
	// (domain.GuildStore contract); commands are copy-then-swap through
	// the actor-authorized guild door, and the dirty unit is the WHOLE guild.
	guilds       map[string]map[int64]domain.GuildRecord
	guildMembers map[string]map[int64][]domain.GuildMemberRecord

	// camps / campMembers are the training-camp plane (the
	// training_camps and training_camp_members tables): the guild
	// plane's twin for the mentor/academy lane. Member slice order is
	// the wire order (domain.TrainingCampStore contract); the dirty
	// unit is the WHOLE camp.
	camps       map[string]map[int64]domain.TrainingCampRecord
	campMembers map[string]map[int64][]domain.TrainingCampMemberRecord

	charDivision map[*domain.Character]string
	changes      changeSet
	ground       groundState

	// defaultSkills is Options.DefaultSkills and serves current-schema
	// character creation.
	defaultSkills SkillSeedFunc
	// defaultQuests is Options.DefaultQuests and is used only at creation.
	defaultQuests QuestSeedFunc
	// defaultInventory is Options.DefaultInventory, used only at creation.
	defaultInventory InventorySeedFunc

	// requireStore is Options.RequireStore (the production posture);
	// CreateCharacter's unseeded-creation refusal keys off it.
	requireStore bool
	// releaseClaim drops this instance's hold on the process-wide
	// single-writer claim (claimAuthority); nil once released by Close.
	releaseClaim func()

	now       func() time.Time
	writeFile func(path string, payload []byte) error // failpoint seam (tests)
	// commitFail is a test failpoint: when non-nil every commit fails
	// with it (the database is never touched), exercising the
	// fail-open-loud path.
	commitFail error

	health        Health
	lastFailLogAt time.Time
	// healthView is health as last published (publishHealthLocked), read by
	// Health without s.mu so a readiness probe never queues behind a writer.
	healthView atomic.Pointer[Health]
}

// failureLogInterval rate-limits repeat write-failure logs (first failure
// always logs at Error).
const failureLogInterval = 30 * time.Second

// Open runs the load ladder: lock -> stray cleanup -> current database
// (quarantine + bak fallback on corruption). An error means the caller must
// refuse to serve.
func Open(dir string, opts Options) (*Store, error) {
	var err error
	dir, err = resolveAuthorityDir(dir)
	if err != nil {
		return nil, err
	}
	if err := ensureAuthorityDirectory(dir); err != nil {
		return nil, fmt.Errorf("authority dir: %w", err)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	s := &Store{
		dir:          dir,
		dbPath:       filepath.Join(dir, DBFileName),
		dbBakPath:    filepath.Join(dir, DBBakFileName),
		characters:   map[string][]*domain.Character{},
		deleted:      map[string][]json.RawMessage{},
		mailboxes:    map[string]map[int64][]domain.LetterRecord{},
		guilds:       map[string]map[int64]domain.GuildRecord{},
		guildMembers: map[string]map[int64][]domain.GuildMemberRecord{},
		camps:        map[string]map[int64]domain.TrainingCampRecord{},
		campMembers:  map[string]map[int64][]domain.TrainingCampMemberRecord{},
		meta:         Meta{NextCharID: map[string]int64{}, NextGuildID: map[string]int64{}},
		charDivision: map[*domain.Character]string{},
		changes:      newChangeSet(),
		ground: groundState{
			loadedRecords: map[string][]domain.GroundItemRecord{},
		},
		defaultSkills:    opts.DefaultSkills,
		defaultQuests:    opts.DefaultQuests,
		defaultInventory: opts.DefaultInventory,
		requireStore:     opts.RequireStore,
		now:              now,
		writeFile:        writeFileAtomic,
	}

	// The claim comes FIRST: everything after it mutates the directory
	// (stray cleanup, quarantine renames, bak refresh), and two racing
	// openers must never perform that surgery concurrently. The claim
	// holds an OS advisory lock on authority.lock for the store's whole
	// lifetime - released by Close, or by the OS at process death (the
	// kill model's free stale-lock reclaim).
	release, warning, err := claimAuthority(dir, now())
	if err != nil {
		return nil, err
	}
	if warning != "" {
		log.Warnf("store: %s", warning)
	}
	s.releaseClaim = release

	for _, target := range []string{s.dbPath, s.dbBakPath} {
		for _, stray := range cleanStrayTemps(target) {
			log.Infof("store: removed stranded temp file %s (a previous writer was killed mid-write; the target itself is whole by construction)", stray)
		}
	}

	if err := s.loadLadder(opts); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// loadLadder picks the current database generation, its recovery copy, or a
// clean first boot.
func (s *Store) loadLadder(opts Options) error {
	dbExists := fileExists(s.dbPath)
	dbBakExists := fileExists(s.dbBakPath)

	if dbExists || dbBakExists {
		return s.loadDBLadder(dbExists)
	}

	if opts.RequireStore {
		return fmt.Errorf("%s missing and %s=1: refusing to boot empty in a store-required environment", s.dbPath, EnvRequireStore)
	}
	log.Infof("store: no %s - atomically initializing an empty development authority (production sets %s=1 and runs sro-init explicitly)", DBFileName, EnvRequireStore)
	if err := createAuthorityDatabase(s.dbPath, emptyAuthoritySeed(s.now().UnixMilli())); err != nil {
		return fmt.Errorf("initializing empty development authority: %w", err)
	}
	db, loaded, err := s.openAndLoadDB(s.dbPath)
	if err != nil {
		return fmt.Errorf("opening initialized development authority: %w", err)
	}
	s.adoptDB(db, loaded)
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// loadDBLadder reads the database with the quarantine -> bak -> refuse
// chain.
func (s *Store) loadDBLadder(mainExists bool) error {
	if !mainExists {
		// A missing MAIN with a live bak is a deleted main or a reboot
		// that landed between a bak recovery and its first commit -
		// never a first boot. The bak rung answers.
		return s.recoverFromDBBak(fmt.Errorf("%s missing while %s exists", DBFileName, DBBakFileName))
	}

	db, loaded, err := s.openAndLoadDB(s.dbPath)
	if err == nil {
		s.adoptDB(db, loaded)
		// Refresh the single bak generation from the database just
		// proven good - once per boot, never while running.
		if bakErr := refreshDBBak(db, s.dbBakPath); bakErr != nil {
			log.Warnf("store: refreshing %s failed: %v", DBBakFileName, bakErr)
		}
		return nil
	}
	if isVersionMismatch(err) {
		// A healthy database this binary cannot serve. Not corruption:
		// the file stays exactly where it is, and the older bak
		// generation must NOT load - that would be a silent rollback.
		// Refuse; the operator swaps the binary.
		return err
	}
	quarantined, qErr := quarantineDB(s.dbPath, s.now())
	if qErr != nil {
		return fmt.Errorf("database unreadable (%v) and quarantine failed (%v)", err, qErr)
	}
	log.Errorf("store: %s failed to load (%v); quarantined to %s", DBFileName, err, quarantined)
	return s.recoverFromDBBak(err)
}

// openAndLoadDB opens one database file and loads it fully; the handle
// is closed on any failure.
func (s *Store) openAndLoadDB(path string) (*sql.DB, *loadedDB, error) {
	db, err := connectDB(path)
	if err != nil {
		return nil, nil, err
	}
	if err := quickCheck(db); err != nil {
		db.Close()
		return nil, nil, err
	}
	loaded, err := loadDB(db, CurrentVersion, CurrentLayoutVersion)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	if err := configureDB(db); err != nil {
		db.Close()
		return nil, nil, err
	}
	return db, loaded, nil
}

// recoverFromDBBak is the ladder's last rung before refusal: restore the
// previous generation into place (so a reboot before the next commit does
// not land on the missing-main rung again), load it, and flag the
// recovery LOUD YELLOW in Health.
func (s *Store) recoverFromDBBak(cause error) error {
	bakPayload, bakReadErr := os.ReadFile(s.dbBakPath)
	if bakReadErr != nil {
		if os.IsNotExist(bakReadErr) {
			return fmt.Errorf("database unusable (%v) and no %s exists: refusing to fabricate state - restore from the quarantine artifacts", cause, DBBakFileName)
		}
		return fmt.Errorf("database unusable (%v) and %s unreadable: %w", cause, DBBakFileName, bakReadErr)
	}
	// The bak is a compact checkpointed image; stale sidecars of the old
	// main generation must not replay into it.
	removeDBSidecars(s.dbPath)
	if writeErr := s.writeFile(s.dbPath, bakPayload); writeErr != nil {
		return fmt.Errorf("database unusable (%v) and restoring %s failed: %w", cause, DBBakFileName, writeErr)
	}
	db, loaded, err := s.openAndLoadDB(s.dbPath)
	if err != nil {
		if isVersionMismatch(err) {
			return fmt.Errorf("%s is for a different binary version: %w", DBBakFileName, err)
		}
		if quarantined, qErr := quarantine(s.dbBakPath, s.now()); qErr == nil {
			log.Errorf("store: %s also failed to load (%v); quarantined to %s", DBBakFileName, err, quarantined)
		}
		return fmt.Errorf("database AND its bak are both unusable (quarantined): refusing to fabricate state - the quarantine artifacts hold the bytes")
	}
	s.adoptDB(db, loaded)
	s.health.LoadedFromBak = true
	s.publishHealthLocked()
	log.Errorf("store: RECOVERED FROM %s (cause: %v) - the store is running on the previous generation (some recent progress may be missing); investigate the quarantine artifacts", DBBakFileName, cause)
	return nil
}

// adoptDB installs a successfully loaded database into the store.
func (s *Store) adoptDB(db *sql.DB, loaded *loadedDB) {
	s.db = db
	s.characters = loaded.characters
	s.characterLookups = nil
	s.deleted = loaded.deleted
	s.ground.loadedRecords = loaded.ground
	s.mailboxes = loaded.mailboxes
	if s.mailboxes == nil {
		s.mailboxes = map[string]map[int64][]domain.LetterRecord{}
	}
	s.guilds = loaded.guilds
	if s.guilds == nil {
		s.guilds = map[string]map[int64]domain.GuildRecord{}
	}
	s.guildMembers = loaded.guildMembers
	if s.guildMembers == nil {
		s.guildMembers = map[string]map[int64][]domain.GuildMemberRecord{}
	}
	s.camps = loaded.camps
	if s.camps == nil {
		s.camps = map[string]map[int64]domain.TrainingCampRecord{}
	}
	s.campMembers = loaded.campMembers
	if s.campMembers == nil {
		s.campMembers = map[string]map[int64][]domain.TrainingCampMemberRecord{}
	}
	s.meta = loaded.meta
	if s.meta.NextCharID == nil {
		s.meta.NextCharID = map[string]int64{}
	}
	if s.meta.NextGuildID == nil {
		s.meta.NextGuildID = map[string]int64{}
	}
	s.ground.loadedGidCounter = loaded.meta.GidCounter
	s.charDivision = map[*domain.Character]string{}
	for divisionID, records := range s.characters {
		for _, c := range records {
			s.charDivision[c] = divisionID
		}
	}
}
