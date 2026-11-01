package quest

import (
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
)

// The shipped v1.150 quest DISPLAY tables this catalog loads. These are
// the only quest content the v1.150 media carries - objective kinds,
// kill targets, counts, turn-in NPCs and rewards were DB-only and never
// shipped (the curated table in definitions.go authors those):
//
//	questdata.txt (224 rows, 11 tab columns, native parser
//	CGlobalDataManager_ParseQuestData via the media-boot walk
//	CGlobalDataManager_LoadTextDataFile case 0x19):
//	  0 service, 1 id, 2 codename, 3 level, 4 korean name,
//	  5 SN_ title symbol, 6 SN_PAY_ popup title, 7 (xxx),
//	  8 SN_PAYCON_ popup body, 9 SN_NN_ notify NPC, 10 SN_NC_ notify
//	  condition. The client gates insertion on level <= 0x5a; the
//	  shipped table's max is far below it.
//
//	questcontentsdata.txt (294 rows, 13 tab columns, parser
//	CGlobalDataManager_ParseQuestContentsData via
//	CGlobalDataManager_LoadTextDataFile case 0x1a, joined onto the
//	questdata record BY CODENAME at media boot):
//	  0 codename, 1 korean name, 2 country byte (payload+0xb0, the
//	  QuestReference_CountryAllowed gate - 3 = both), 3 next-quest
//	  codenames (comma separated, "xxx" = none - the quest CHAIN),
//	  4 give-up warn byte (payload+0xb1, the
//	  CIFQuestReward_ConfirmReward variant), 5..12 SN_CON_*
//	  contents-line symbols ("xxx" = none) - the textquest symbols
//	  the wire's SQuestContents description field references.

// questdataColumns is the shipped questdata.txt column count.
const questdataColumns = 11

// questcontentsColumns is the shipped questcontentsdata.txt column count.
const questcontentsColumns = 13

// textdataNone is the media's "no value" cell sentinel.
const textdataNone = "xxx"

// CatalogRow is one shipped questdata.txt row's server-consumed fields.
type CatalogRow struct {
	// ID is the v1.150-native quest ref id (col 1) - the wire id every
	// 0x31ED/0x32B3 emission carries and the 0x71EB/0x729A bodies name.
	ID uint32
	// Codename is the version-stable join key (col 2).
	Codename string
	// Level is the quest level byte (col 3; the client's content-button
	// delta art and the <= 0x5a insertion gate read it).
	Level uint8
	// TitleSymbol is questdata col 5, the localized row inserted into a
	// server-driven NPC quest option list.
	TitleSymbol string
}

// ContentsRow is one shipped questcontentsdata.txt row's server-consumed
// fields (joined by codename, exactly like the native media-boot join).
type ContentsRow struct {
	Codename string
	// CountryByte is col 2 (the QuestReference_CountryAllowed gate;
	// 3 = both).
	CountryByte uint8
	// NextQuests are the col-3 chain codenames ("xxx" = none).
	NextQuests []string
	// GiveupWarnByte is col 4 (the CIFQuestReward confirm variant).
	GiveupWarnByte uint8
	// ContentsSymbols are the non-"xxx" col 5..12 SN_CON_* symbols, in
	// column order - the textquest symbols a wire SQuestContents node's
	// description field carries.
	ContentsSymbols []string
}

// Catalog is the lazy loader over the two shipped tables. Degrades to an
// empty catalog when the textdata is absent (the TextdataSkills posture:
// the server still boots; definition loading then resolves nothing and
// the quest plane runs definition-less, refusing every inbound id).
type Catalog struct {
	dir string

	once       sync.Once
	rows       map[string]CatalogRow
	rowsByID   map[uint32]string
	contents   map[string]ContentsRow
	loadedRows int
}

// NewCatalog returns a lazy loader over the verified projection's textdata directory.
func NewCatalog(dir string) *Catalog {
	return &Catalog{dir: dir}
}

// Dir answers the resolved textdata directory (the wiring shares it with
// the sibling loaders it constructs alongside this catalog).
func (c *Catalog) Dir() string {
	return c.dir
}

// QuestByCodename resolves a shipped questdata row by its codename;
// ok=false when the table has no such row (callers fail loud - see
// LoadDefinitions).
func (c *Catalog) QuestByCodename(codename string) (CatalogRow, bool) {
	c.once.Do(c.load)
	row, ok := c.rows[codename]
	return row, ok
}

// ContentsByCodename resolves the questcontentsdata join row; ok=false
// when the shipped table carries none for the codename.
func (c *Catalog) ContentsByCodename(codename string) (ContentsRow, bool) {
	c.once.Do(c.load)
	row, ok := c.contents[codename]
	return row, ok
}

// Len reports how many questdata rows loaded (0 = textdata absent).
func (c *Catalog) Len() int {
	c.once.Do(c.load)
	return c.loadedRows
}

func (c *Catalog) load() {
	c.rows = map[string]CatalogRow{}
	c.rowsByID = map[uint32]string{}
	c.contents = map[string]ContentsRow{}

	for _, fields := range enterworld.ReadTextdataFile(filepath.Join(c.dir, "questdata.txt")) {
		if len(fields) < questdataColumns {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimSpace(fields[1]), 10, 32)
		if err != nil || id == 0 {
			continue
		}
		level, err := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 8)
		if err != nil {
			continue
		}
		codename := strings.TrimSpace(fields[2])
		if codename == "" {
			continue
		}
		if first, dup := c.rows[codename]; dup {
			log.Warnf("quest: questdata codename %q is ambiguous (ids %d and %d); keeping the first", codename, first.ID, id)
			continue
		}
		c.rows[codename] = CatalogRow{
			ID: uint32(id), Codename: codename, Level: uint8(level),
			TitleSymbol: strings.TrimSpace(fields[5]),
		}
		c.rowsByID[uint32(id)] = codename
		c.loadedRows++
	}

	for _, fields := range enterworld.ReadTextdataFile(filepath.Join(c.dir, "questcontentsdata.txt")) {
		if len(fields) < questcontentsColumns {
			continue
		}
		codename := strings.TrimSpace(fields[0])
		if codename == "" {
			continue
		}
		row := ContentsRow{Codename: codename}
		if country, err := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 8); err == nil {
			row.CountryByte = uint8(country)
		}
		if next := strings.TrimSpace(fields[3]); next != "" && next != textdataNone {
			for _, chained := range strings.Split(next, ",") {
				if trimmed := strings.TrimSpace(chained); trimmed != "" {
					row.NextQuests = append(row.NextQuests, trimmed)
				}
			}
		}
		if warn, err := strconv.ParseUint(strings.TrimSpace(fields[4]), 10, 8); err == nil {
			row.GiveupWarnByte = uint8(warn)
		}
		for column := 5; column < questcontentsColumns; column++ {
			symbol := strings.TrimSpace(fields[column])
			if symbol != "" && symbol != textdataNone {
				row.ContentsSymbols = append(row.ContentsSymbols, symbol)
			}
		}
		if _, dup := c.contents[codename]; !dup {
			c.contents[codename] = row
		}
	}

	if c.loadedRows == 0 {
		log.Warnf("quest: questdata not found under verified projection %s; the quest plane runs definition-less and refuses every request", c.dir)
		return
	}
	log.Infof("quest: questdata loaded from %s (%d quest row(s), %d contents row(s))", c.dir, c.loadedRows, len(c.contents))
}
