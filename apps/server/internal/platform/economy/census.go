/*
===========================================================================

census.go - the one-time staff seal census of the live shard (port-only)

Port-only, not native (#1691, owner-approved from the #1604 P13 vote).
A staff action runs it on demand: the store copies itself (store
.SnapshotTo, which holds off no door and never runs inside one), the
census is computed from that copy here, and the copy is discarded. It
never runs on the tick and never reads the live authority.

What it reports: how many Seal of Star, Moon and Sun items exist on the
shard, by degree and tier, across character inventories, avatar rows,
companion and transport containers, account warehouses, guild storage
and ground items - and how many distinct characters hold at least one.
Name-free end to end: no character, account or item identity is ever
written into the report; holder identities live only in this function's
stack while it counts. A (degree, tier) bucket whose holder count is
under MinSealCensusHolders is withheld whole, like the kill-stats floor
(#1639): its items could single out one player's hoard. Every number
the report publishes - the totals, the per-location counts, the holder
headlines - sums the shown buckets only, so a withheld bucket's size
cannot be recovered by subtraction; the report says how many buckets
were withheld, never how much they held.

The route exists only while the shard allows it (SRO_SEAL_CENSUS on);
off (the default), it answers 404 like any unknown route.

===========================================================================
*/
package economy

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// EnvSealCensus turns the staff seal census on ("on", "1", "true");
// anything else, including a typo, keeps it off.
const EnvSealCensus = "SRO_SEAL_CENSUS"

// MinSealCensusHolders is the privacy floor: a (degree, tier) bucket
// with fewer distinct holders than this is never served.
const MinSealCensusHolders = 3

// censusFile is the throwaway snapshot copy one census run reads.
const censusFile = "economy-census.db"

// errCensusRunning answers a second run while one is in flight.
var errCensusRunning = errors.New("a seal census is already running")

/*
================
SealCensusFromEnv

The wiring's word that the shard allows the census (SRO_SEAL_CENSUS).
================
*/
func SealCensusFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvSealCensus))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
Service.SetSealCensusEnabled

Only then does the route exist.
================
*/
func (s *Service) SetSealCensusEnabled(enabled bool) {
	s.sealCensus = enabled
}

//============================================================================

/*
================
SealCensusCell

One (degree, tier) bucket: how many seal items exist and how many
distinct holders hold them. A nil cell in a served row was withheld
under the privacy floor.
================
*/
type SealCensusCell struct {
	Items   int `json:"items"`
	Holders int `json:"holders"`
}

/*
================
SealCensusRow

One degree's three tiers (Star, Moon, Sun).
================
*/
type SealCensusRow struct {
	Degree int             `json:"degree"`
	Star   *SealCensusCell `json:"star"`
	Moon   *SealCensusCell `json:"moon"`
	Sun    *SealCensusCell `json:"sun"`
}

/*
================
SealCensusLocations

Where the shown seals sit, all shown tiers and degrees together. A
withheld bucket's items are absent here too, so the fields sum to the
report's TotalItems.
================
*/
type SealCensusLocations struct {
	Inventory    int `json:"inventory"`
	Avatar       int `json:"avatar"`
	Pet          int `json:"pet"`
	Transport    int `json:"transport"`
	Storage      int `json:"storage"`
	GuildStorage int `json:"guildStorage"`
	Ground       int `json:"ground"`
}

/*
================
SealCensus

The contract v1 census report. Rows holds only degrees with at least
one served bucket; SuppressedCells counts the withheld buckets - a
count of buckets, never of items. TotalItems, Locations and the two
holder headlines all cover the shown buckets only.
================
*/
type SealCensus struct {
	Contract          int                 `json:"contract"`
	TakenAt           int64               `json:"takenAt"`
	ElapsedMs         int64               `json:"elapsedMs"`
	MinHolders        int                 `json:"minHolders"`
	Rows              []SealCensusRow     `json:"rows"`
	SuppressedCells   int                 `json:"suppressedCells"`
	Locations         SealCensusLocations `json:"locations"`
	TotalItems        int                 `json:"totalItems"`
	CharactersHolding int                 `json:"charactersHolding"`
	AccountsHolding   int                 `json:"accountsHolding"`
}

// censusBucket is one (degree, tier) being counted: its items, its
// holders and where its items sit, so a withheld bucket leaves no
// trace in any served total.
type censusBucket struct {
	items   int
	holders map[string]bool
	places  SealCensusLocations
}

// The counted container kinds, as censusBucket.addPlace switches on.
const (
	placeInventory = iota
	placeAvatar
	placePet
	placeTransport
	placeStorage
	placeGuild
	placeGround
)

/*
================
censusBucket.addPlace

One more of this bucket's items in place.
================
*/
func (b *censusBucket) addPlace(place int) {
	switch place {
	case placeInventory:
		b.places.Inventory++
	case placeAvatar:
		b.places.Avatar++
	case placePet:
		b.places.Pet++
	case placeTransport:
		b.places.Transport++
	case placeStorage:
		b.places.Storage++
	case placeGuild:
		b.places.GuildStorage++
	case placeGround:
		b.places.Ground++
	}
}

// censusTally accumulates one copy's counts. Holder keys are internal
// only ("char/division/id", "acct/id") and never leave this owner.
type censusTally struct {
	buckets map[[2]int]*censusBucket
}

/*
================
newCensusTally
================
*/
func newCensusTally() *censusTally {
	return &censusTally{buckets: map[[2]int]*censusBucket{}}
}

// censusItem is an inventory-shaped row as the census reads it.
type censusItem struct {
	Codename string     `json:"codename"`
	Summon   *censusCOS `json:"summon,omitempty"`
}

// censusCOS is a companion's persisted shape: its kind and container.
type censusCOS struct {
	Codename  string `json:"codename"`
	Container *struct {
		Rows []censusItem `json:"rows"`
	} `json:"container,omitempty"`
}

/*
================
censusTally.countItem

One row: a seal when the codename says so (the distribution's own
recognition, sealCodename), filed into its (degree, tier) bucket under
holder ("" for none) and its place inside that bucket. Equipment rows
never stack, so one row is one item, as the distribution counts them.
================
*/
func (t *censusTally) countItem(item censusItem, holder string, place int) {
	match := sealCodename.FindStringSubmatch(item.Codename)
	if match == nil {
		return
	}
	degree := 0
	for _, digit := range match[1] {
		degree = degree*10 + int(digit-'0')
	}
	tier := strings.Index("ABC", match[2])
	key := [2]int{degree, tier}
	bucket := t.buckets[key]
	if bucket == nil {
		bucket = &censusBucket{holders: map[string]bool{}}
		t.buckets[key] = bucket
	}
	bucket.items++
	bucket.addPlace(place)
	if holder != "" {
		bucket.holders[holder] = true
	}
}

/*
================
censusTally.countRows

A container's rows under one holder; a row's own summon container is a
companion's bag travelling with it.
================
*/
func (t *censusTally) countRows(rows []censusItem, holder string, place int) {
	for _, row := range rows {
		t.countItem(row, holder, place)
		if row.Summon != nil {
			t.countCOS(row.Summon, holder)
		}
	}
}

/*
================
censusTally.countCOS

A companion's container: transports (COS_T_*) carry cargo, every other
companion is a pet. The holder is the owning character ("" when the
summoner item lies on the ground).
================
*/
func (t *censusTally) countCOS(cos *censusCOS, holder string) {
	if cos == nil || cos.Container == nil {
		return
	}
	if strings.HasPrefix(cos.Codename, "COS_T_") {
		for _, row := range cos.Container.Rows {
			t.countItem(row, holder, placeTransport)
		}
		return
	}
	for _, row := range cos.Container.Rows {
		t.countItem(row, holder, placePet)
	}
}

/*
================
readSealCensus

The whole census from one snapshot copy, as of at.
================
*/
func readSealCensus(db *sql.DB, at time.Time) (*SealCensus, error) {
	tally := newCensusTally()
	if err := tally.readCharacters(db); err != nil {
		return nil, err
	}
	if err := tally.readWarehouses(db); err != nil {
		return nil, err
	}
	if err := tally.readGuilds(db); err != nil {
		return nil, err
	}
	if err := tally.readGround(db); err != nil {
		return nil, err
	}
	return tally.report(at), nil
}

/*
================
censusTally.readCharacters

Every character's inventory, avatar rows and companions. The
character key (division/id) and account are stack-local identities:
holders are counted, never named.
================
*/
func (t *censusTally) readCharacters(db *sql.DB) error {
	rows, err := db.Query("SELECT division, id, record FROM characters")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var division, raw string
		var id int64
		if err := rows.Scan(&division, &id, &raw); err != nil {
			return err
		}
		var c struct {
			AccountID        string       `json:"accountId"`
			MissionInventory []censusItem `json:"missionInventory"`
			AvatarInventory  *struct {
				Rows []censusItem `json:"rows"`
			} `json:"avatarInventory,omitempty"`
			ActiveCOS   *censusCOS   `json:"activeCos,omitempty"`
			Mercenaries []*censusCOS `json:"mercenaries,omitempty"`
		}
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return fmt.Errorf("seal census: %w", err)
		}
		holder := "char/" + division + "/" + fmt.Sprint(id)
		t.countRows(c.MissionInventory, holder, placeInventory)
		if c.AvatarInventory != nil {
			t.countRows(c.AvatarInventory.Rows, holder, placeAvatar)
		}
		t.countCOS(c.ActiveCOS, holder)
		for _, mercenary := range c.Mercenaries {
			t.countCOS(mercenary, holder)
		}
	}
	return rows.Err()
}

/*
================
censusTally.readWarehouses

The account warehouses: their holder is the account, whose characters
share it, so the bucket's holder unit is the account itself.
================
*/
func (t *censusTally) readWarehouses(db *sql.DB) error {
	rows, err := db.Query("SELECT account_id, record FROM account_storage")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var account, raw string
		if err := rows.Scan(&account, &raw); err != nil {
			return err
		}
		var storage struct {
			Rows []censusItem `json:"rows"`
		}
		if err := json.Unmarshal([]byte(raw), &storage); err != nil {
			return fmt.Errorf("seal census: %w", err)
		}
		holder := "acct/" + account
		t.countRows(storage.Rows, holder, placeStorage)
	}
	return rows.Err()
}

/*
================
censusTally.readGuilds

Guild warehouses count toward what exists, never toward a holder.
================
*/
func (t *censusTally) readGuilds(db *sql.DB) error {
	rows, err := db.Query("SELECT record FROM guilds")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var guild struct {
			Storage *struct {
				Rows []censusItem `json:"rows"`
			} `json:"storage,omitempty"`
		}
		if err := json.Unmarshal([]byte(raw), &guild); err != nil {
			return fmt.Errorf("seal census: %w", err)
		}
		if guild.Storage != nil {
			t.countRows(guild.Storage.Rows, "", placeGuild)
		}
	}
	return rows.Err()
}

/*
================
censusTally.readGround

Ground items and the companion bags lying among them; no holder.
================
*/
func (t *censusTally) readGround(db *sql.DB) error {
	rows, err := db.Query("SELECT record FROM ground_items")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var item censusItem
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return fmt.Errorf("seal census: %w", err)
		}
		t.countItem(item, "", placeGround)
		if item.Summon != nil {
			t.countCOS(item.Summon, "")
		}
	}
	return rows.Err()
}

/*
================
censusTally.report

The served shape: buckets under the floor are withheld whole and
counted as buckets; degrees with nothing served drop out of the rows.
Every total below sums the shown buckets only - the items, the places
and the holder headlines alike - so a withheld bucket's size cannot be
recovered by subtraction from anything the report publishes.
================
*/
func (t *censusTally) report(at time.Time) *SealCensus {
	degrees := map[int]*SealCensusRow{}
	chars, accounts := map[string]bool{}, map[string]bool{}
	var locations SealCensusLocations
	suppressed, total := 0, 0
	for key, bucket := range t.buckets {
		if len(bucket.holders) < MinSealCensusHolders {
			suppressed++
			continue
		}
		cell := &SealCensusCell{Items: bucket.items, Holders: len(bucket.holders)}
		row := degrees[key[0]]
		if row == nil {
			row = &SealCensusRow{Degree: key[0]}
			degrees[key[0]] = row
		}
		switch key[1] {
		case 0:
			row.Star = cell
		case 1:
			row.Moon = cell
		case 2:
			row.Sun = cell
		}
		total += bucket.items
		locations.Inventory += bucket.places.Inventory
		locations.Avatar += bucket.places.Avatar
		locations.Pet += bucket.places.Pet
		locations.Transport += bucket.places.Transport
		locations.Storage += bucket.places.Storage
		locations.GuildStorage += bucket.places.GuildStorage
		locations.Ground += bucket.places.Ground
		for holder := range bucket.holders {
			if strings.HasPrefix(holder, "acct/") {
				accounts[holder] = true
			} else {
				chars[holder] = true
			}
		}
	}
	rows := []SealCensusRow{}
	for _, row := range degrees {
		rows = append(rows, *row)
	}
	slices.SortFunc(rows, func(a, b SealCensusRow) int { return a.Degree - b.Degree })
	return &SealCensus{
		Contract:          contractVersion,
		TakenAt:           at.Unix(),
		MinHolders:        MinSealCensusHolders,
		Rows:              rows,
		SuppressedCells:   suppressed,
		Locations:         locations,
		TotalItems:        total,
		CharactersHolding: len(chars),
		AccountsHolding:   len(accounts),
	}
}

//============================================================================

/*
================
Service.RunSealCensus

One census now: the store copies itself outside every door, the copy
is read once and deleted, and the report becomes the newest answer.
A second run while one is in flight is refused, never queued.
================
*/
func (s *Service) RunSealCensus() (*SealCensus, error) {
	if !s.censusMu.TryLock() {
		return nil, errCensusRunning
	}
	defer s.censusMu.Unlock()
	started := time.Now()
	path := filepath.Join(s.dir, censusFile)
	if err := s.store.SnapshotTo(path); err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(path) }()
	db, err := openSnapshot(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	report, err := readSealCensus(db, s.now())
	if err != nil {
		return nil, err
	}
	report.ElapsedMs = time.Since(started).Milliseconds()
	s.mu.Lock()
	s.censusLatest = report
	s.mu.Unlock()
	return report, nil
}

/*
================
Service.serveSealCensus

POST runs the census and answers with it; GET answers with the newest
report, or no_census until one exists.
================
*/
func (s *Service) serveSealCensus(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		report, err := s.RunSealCensus()
		if errors.Is(err, errCensusRunning) {
			writeError(w, http.StatusConflict, queryError{"census_running", "a seal census is already running"})
			return
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, report)
		return
	}
	s.mu.Lock()
	report := s.censusLatest
	s.mu.Unlock()
	if report == nil {
		writeError(w, http.StatusServiceUnavailable, queryError{"no_census", "no seal census has been run yet"})
		return
	}
	writeJSON(w, report)
}
