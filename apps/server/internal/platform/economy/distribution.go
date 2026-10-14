/*
===========================================================================

distribution.go - who holds the gold and the gear, from one snapshot

Computed once per snapshot (Service.Snapshot) and served as
/economy/v1/distribution. Gold is the characters' gold plus, for the whole
shard only, the account warehouses' (a warehouse belongs to an account,
not a division). An account is active when any of its characters entered
the world in the 14 days before the snapshot. Seal items are recognised
by their codename: _A_RARE is a Seal of Star, _B_ of Moon, _C_ of Sun,
after the item's two-digit degree.

===========================================================================
*/
package economy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"time"

	"opensro.online/server/internal/game/item/wire"
)

// activeWindow is how recent a world entry makes an account active.
const activeWindow = 14 * 24 * time.Hour

// sealCodename picks a seal item's degree and its Star/Moon/Sun letter.
var sealCodename = regexp.MustCompile(`_(\d{2})(?:_[A-Z]{2})?_([ABC])_RARE$`)

/*
================
Distribution

The contract v1 distribution block. goldTotal feeds the gold history.
================
*/
type Distribution struct {
	SnapshotAt     int64            `json:"snapshotAt"`
	Gold           GoldDistribution `json:"gold"`
	Plus           map[string]int   `json:"plus"`
	Sox            []SoxRow         `json:"sox"`
	SoxAccounts14d []SoxRow         `json:"soxAccounts14d"`
	goldTotal      int64
}

/*
================
GoldDistribution

Amounts are decimal strings (contract v1: they exceed JS numbers).
================
*/
type GoldDistribution struct {
	Total          string  `json:"total"`
	Characters     int     `json:"characters"`
	Accounts14d    int     `json:"accounts14d"`
	GiniCharacter  float64 `json:"giniCharacter"`
	GiniAccount14d float64 `json:"giniAccount14d"`
	Top1Share      float64 `json:"top1Share"`
	Top10Share     float64 `json:"top10Share"`
	P50            string  `json:"p50"`
	P90            string  `json:"p90"`
	P99            string  `json:"p99"`
	Max            string  `json:"max"`
}

/*
================
SoxRow
================
*/
type SoxRow struct {
	Degree int `json:"degree"`
	Star   int `json:"star"`
	Moon   int `json:"moon"`
	Sun    int `json:"sun"`
}

/*
================
heldItem

The fields of an inventory row the distribution reads.
================
*/
type heldItem struct {
	Codename  string `json:"codename"`
	TypeFlags uint16 `json:"typeFlags"`
	Plus      int64  `json:"plus"`
}

/*
================
heldCharacter
================
*/
type heldCharacter struct {
	AccountID        string     `json:"accountId"`
	Gold             *int64     `json:"gold"`
	LastLoginAtMs    int64      `json:"lastLoginAtMs"`
	MissionInventory []heldItem `json:"missionInventory"`
}

/*
================
tally

One block's running counts while the snapshot is read.
================
*/
type tally struct {
	characterGold  []int64
	accountGold    map[string]int64
	active         map[string]bool
	plus           map[int64]int
	sox, soxActive map[int]*SoxRow
	total          int64
}

/*
================
newTally
================
*/
func newTally() *tally {
	return &tally{accountGold: map[string]int64{}, active: map[string]bool{}, plus: map[int64]int{},
		sox: map[int]*SoxRow{}, soxActive: map[int]*SoxRow{}}
}

/*
================
readDistribution

Every division's block and the whole shard's (key ""), as of at.
================
*/
func readDistribution(db *sql.DB, at time.Time) (map[string]Distribution, error) {
	cutoff := at.Add(-activeWindow).UnixMilli()
	tallies := map[string]*tally{"": newTally()}
	// Items count once every character is read: an account's activity is
	// only known then.
	type owned struct {
		division, account string
		items             []heldItem
	}
	var holdings []owned
	rows, err := db.Query("SELECT division, record FROM characters")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var division, raw string
		if err := rows.Scan(&division, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var c heldCharacter
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			rows.Close()
			return nil, fmt.Errorf("economy distribution: %w", err)
		}
		if tallies[division] == nil {
			tallies[division] = newTally()
		}
		gold := int64(0)
		if c.Gold != nil && *c.Gold > 0 {
			gold = *c.Gold
		}
		for _, t := range []*tally{tallies[""], tallies[division]} {
			t.characterGold = append(t.characterGold, gold)
			t.accountGold[c.AccountID] += gold
			t.total += gold
			if c.LastLoginAtMs >= cutoff {
				t.active[c.AccountID] = true
			}
		}
		holdings = append(holdings, owned{division: division, account: c.AccountID, items: c.MissionInventory})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	all := tallies[""]
	for _, held := range holdings {
		for _, t := range []*tally{all, tallies[held.division]} {
			t.countItems(held.items, t.active[held.account])
		}
	}
	if err := all.readWarehouses(db); err != nil {
		return nil, err
	}
	blocks := make(map[string]Distribution, len(tallies))
	for division, t := range tallies {
		blocks[division] = t.block(at)
	}
	return blocks, nil
}

/*
================
tally.readWarehouses

The account warehouses join the whole shard's block: their gold counts
toward the total and the account's share, their items toward the gear.
================
*/
func (t *tally) readWarehouses(db *sql.DB) error {
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
			Gold int64      `json:"gold"`
			Rows []heldItem `json:"rows"`
		}
		if err := json.Unmarshal([]byte(raw), &storage); err != nil {
			return fmt.Errorf("economy distribution: %w", err)
		}
		if storage.Gold > 0 {
			t.accountGold[account] += storage.Gold
			t.total += storage.Gold
		}
		t.countItems(storage.Rows, t.active[account])
	}
	return rows.Err()
}

/*
================
tally.countItems
================
*/
func (t *tally) countItems(items []heldItem, active bool) {
	for _, item := range items {
		if !wire.IsEquipmentBand(item.TypeFlags) {
			continue
		}
		t.plus[item.Plus]++
		match := sealCodename.FindStringSubmatch(item.Codename)
		if match == nil {
			continue
		}
		degree, _ := strconv.Atoi(match[1])
		countSeal(t.sox, degree, match[2])
		if active {
			countSeal(t.soxActive, degree, match[2])
		}
	}
}

/*
================
countSeal
================
*/
func countSeal(rows map[int]*SoxRow, degree int, letter string) {
	row := rows[degree]
	if row == nil {
		row = &SoxRow{Degree: degree}
		rows[degree] = row
	}
	switch letter {
	case "A":
		row.Star++
	case "B":
		row.Moon++
	case "C":
		row.Sun++
	}
}

/*
================
tally.block
================
*/
func (t *tally) block(at time.Time) Distribution {
	characters := slices.Clone(t.characterGold)
	slices.Sort(characters)
	var accounts []int64
	for account, gold := range t.accountGold {
		if t.active[account] {
			accounts = append(accounts, gold)
		}
	}
	slices.Sort(accounts)
	plus := make(map[string]int, len(t.plus))
	for level, count := range t.plus {
		plus[strconv.FormatInt(level, 10)] = count
	}
	return Distribution{
		SnapshotAt: at.Unix(),
		Gold: GoldDistribution{
			Total:          strconv.FormatInt(t.total, 10),
			Characters:     len(characters),
			Accounts14d:    len(accounts),
			GiniCharacter:  gini(characters),
			GiniAccount14d: gini(accounts),
			Top1Share:      topShare(characters, 0.01),
			Top10Share:     topShare(characters, 0.10),
			P50:            strconv.FormatInt(percentile(characters, 0.50), 10),
			P90:            strconv.FormatInt(percentile(characters, 0.90), 10),
			P99:            strconv.FormatInt(percentile(characters, 0.99), 10),
			Max:            strconv.FormatInt(percentile(characters, 1), 10),
		},
		Plus:           plus,
		Sox:            sealRows(t.sox),
		SoxAccounts14d: sealRows(t.soxActive),
		goldTotal:      t.total,
	}
}

/*
================
sealRows

By degree, ascending; never null in JSON.
================
*/
func sealRows(rows map[int]*SoxRow) []SoxRow {
	out := make([]SoxRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	slices.SortFunc(out, func(a, b SoxRow) int { return a.Degree - b.Degree })
	return out
}

/*
================
gini

The Gini coefficient of ascending non-negative holdings: 0 when everyone
holds the same, towards 1 when one holder has it all.
================
*/
func gini(sorted []int64) float64 {
	n := float64(len(sorted))
	var sum, weighted float64
	for i, value := range sorted {
		sum += float64(value)
		weighted += float64(i+1) * float64(value)
	}
	if n == 0 || sum == 0 {
		return 0
	}
	return 2*weighted/(n*sum) - (n+1)/n
}

/*
================
topShare

The share of the total that the top fraction of ascending holders own
(at least one holder).
================
*/
func topShare(sorted []int64, fraction float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	count := max(1, int(math.Ceil(float64(len(sorted))*fraction)))
	var top, sum float64
	for i, value := range sorted {
		sum += float64(value)
		if i >= len(sorted)-count {
			top += float64(value)
		}
	}
	if sum == 0 {
		return 0
	}
	return top / sum
}

/*
================
percentile

The nearest-rank percentile of ascending values, 0 when there are none.
================
*/
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := max(1, int(math.Ceil(p*float64(len(sorted)))))
	return sorted[rank-1]
}

/*
================
sortStrings
================
*/
func sortStrings(values []string) {
	slices.Sort(values)
}
