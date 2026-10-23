package main

import (
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/game/world/monster"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 2 && len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: population-audit <textdata-directory> [<derived-directory>]")
		os.Exit(2)
	}
	derivedDir := ""
	if len(os.Args) == 3 {
		derivedDir = os.Args[2]
	}
	if _, err := os.Stat(filepath.Join(os.Args[1], "npcpos.txt")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	t, err := monster.LoadTemplateFrom(os.Args[1], derivedDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rows := []any{}
	unmatched := map[string]int{}
	total := 0
	familySlots := map[string]int{}
	type familyAudit struct {
		Anchors int   `json:"anchors"`
		Matched int   `json:"matched"`
		Slots   int   `json:"slotUpperBound"`
		Country uint8 `json:"country"`
		Level   uint8 `json:"level"`
	}
	europe := map[string]*familyAudit{}
	for _, n := range t.Nests {
		r := t.Refs[n.RefObjID]
		total += n.InstanceLimit()
		familySlots[r.Codename] += n.InstanceLimit()
		if strings.HasPrefix(r.Codename, "MOB_EU_") {
			a := europe[r.Codename]
			if a == nil {
				a = &familyAudit{Country: r.Country, Level: r.Level}
				europe[r.Codename] = a
			}
			a.Anchors++
			a.Slots += n.InstanceLimit()
			if n.RetailEvidence {
				a.Matched++
			}
		}
		if !n.RetailEvidence {
			unmatched[r.Codename]++
		}
		if r.MonsterType&15 == 3 {
			rows = append(rows, map[string]any{"code": r.Codename, "nest": n})
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"europeanFamilies": europe, "nests": len(t.Nests), "matched": t.EvidenceMatches, "slots": total, "unmatched": unmatched, "uniques": rows, "familySlotUpperBounds": familySlots}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
