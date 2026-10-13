/*
===========================================================================

objective_text_counts_test.go - required counts agree with the shipped text

Players compared quest counts with guides written for later versions
(#862). The v1.150 client's own text decides: where an objective line
prints a number ("Deliver 50 sturdy paddles", "Hunt 20 Movias (%d)"),
the count this server requires must be that number. A later version's
rebalanced count is never adopted.

===========================================================================
*/
package quest

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// textquestEnglishColumn is textquest.txt's English text (col 1 is the
// symbol, as npcworlddata reads textdataname).
const textquestEnglishColumn = 8

// objectiveTextNumber matches a standalone number; a digit inside a word
// ("2nd", "Rahid5") is not one.
var objectiveTextNumber = regexp.MustCompile(`\b(\d{1,5})\b`)

/*
================
objectiveCount

One objective line's symbol and the count the server requires for it.
================
*/
type objectiveCount struct {
	quest, symbol string
	count         uint32
}

/*
================
specObjectiveCounts

Every counted objective of a quest spec: its own collect, kill or delivery
and each required mission. An optional mission counts past the class's
required total (its raw drops before conversion), so it has no line of
its own to agree with.
================
*/
func specObjectiveCounts(codename string, spec QuestSpec, symbol string) []objectiveCount {
	var out []objectiveCount
	add := func(symbol string, count uint32) {
		if symbol != "" && count > 0 {
			out = append(out, objectiveCount{codename, symbol, count})
		}
	}
	add(symbol, spec.CollectCount+spec.KillCount)
	if len(spec.DeliveryItems) == 1 {
		add(symbol, spec.DeliveryItems[0].Count)
	}
	for _, mission := range spec.Objectives {
		if !mission.Optional {
			add(mission.ContentsSymbol, mission.CollectCount+mission.KillCount)
		}
	}
	return out
}

/*
================
TestObjectiveCountsMatchTheirShippedText
================
*/
func TestObjectiveCountsMatchTheirShippedText(t *testing.T) {
	textdata := gamedatatest.TextdataDir(t)
	defs, err := LoadDefinitions(NewCatalog(textdata), enterworld.NewTextdataItems(textdata))
	if err != nil {
		t.Fatal(err)
	}
	english := map[string]string{}
	for _, cols := range enterworld.ReadTextdataFile(filepath.Join(textdata, "textquest.txt")) {
		if len(cols) > textquestEnglishColumn {
			english[strings.TrimSpace(cols[1])] = cols[textquestEnglishColumn]
		}
	}
	var counts []objectiveCount
	for _, def := range defs.ordered {
		counts = append(counts, specObjectiveCounts(def.Codename, def.QuestSpec, def.ContentsSymbol)...)
		for _, stage := range def.Stages {
			counts = append(counts, specObjectiveCounts(def.Codename, stage.QuestSpec, stage.ContentsSymbol)...)
		}
	}
	checked := 0
	for _, c := range counts {
		var printed []uint32
		for _, match := range objectiveTextNumber.FindAllStringSubmatch(english[c.symbol], -1) {
			n, _ := strconv.ParseUint(match[1], 10, 32)
			printed = append(printed, uint32(n))
		}
		if len(printed) == 0 {
			continue
		}
		checked++
		found := false
		for _, n := range printed {
			found = found || n == c.count
		}
		if !found {
			t.Errorf("%s %s requires %d, its text reads %q", c.quest, c.symbol, c.count, english[c.symbol])
		}
	}
	// The shipped data has well over a hundred numbered objective lines; a
	// broken text join must not pass by checking none.
	if checked < 100 {
		t.Fatalf("only %d numbered objective lines checked", checked)
	}
}
