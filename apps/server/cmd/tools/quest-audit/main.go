package main

import (
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/quest"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: quest-audit <primary-textdata-directory> [<derived-data-directory>]")
		os.Exit(2)
	}
	dir := os.Args[1]
	derived := ""
	if len(os.Args) > 2 {
		derived = os.Args[2]
	}
	catalog := quest.NewCatalogWithDerived(dir, derived)
	defs, err := quest.LoadDefinitions(catalog, enterworld.NewTextdataItems(dir))
	if err != nil {
		panic(err)
	}
	if catalog.Len() == 0 {
		panic("primary quest catalog unavailable")
	}
	missing := []string{}
	blocked := map[string]string{}
	external := map[string][]string{}
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(dir, "questdata.txt")) {
		if len(row) < 3 || row[0] != "1" {
			continue
		}
		if _, ok := defs.ByCodename(row[2]); !ok {
			missing = append(missing, row[2])
		}
	}
	for _, def := range defs.All() {
		if def.AcceptanceUnavailable != "" {
			blocked[def.Codename] = def.AcceptanceUnavailable
		}
		for _, code := range def.RequiredQuests {
			if _, ok := defs.ByCodename(code); !ok {
				external[def.Codename] = append(external[def.Codename], code)
			}
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"primaryDisplayRows": catalog.Len(), "executableDefinitions": defs.Len(), "missingDefinitions": missing, "blockedAcceptance": blocked, "unimplementedPredecessors": external}); err != nil {
		panic(err)
	}
}
