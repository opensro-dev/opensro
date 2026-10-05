// Package instance owns authored world definitions.
package instance

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

type DefinitionID uint16

// Definition preserves all fields loaded by v1.150 64DF40. The server's
// 5EB500/5EB830 consume +22 and 5EC010 consumes +24. NativeType is retained
// without guessing that every nonzero value has the same lifecycle policy.
type Definition struct {
	ID          DefinitionID
	CodeName    string
	NativeType  uint8
	LayerLimit  uint16
	PlayerLimit uint16
	Strings     [20]string
	Numbers     [20]int32
}

//go:embed data/v1150_gameworld.tsv
var shippedData string

var shipped = mustParseShipped()

func mustParseShipped() []Definition {
	rows, err := parseDefinitions(shippedData)
	if err != nil {
		panic(err)
	}
	return rows
}

// Shipped returns value copies in authored row order. Arrays deliberately
// keep callers from retaining mutable aliases into the catalog.
func Shipped() []Definition { return append([]Definition(nil), shipped...) }

// siegeWorldCodes are the RefGameWorld codenames CGameWorldMgr_CreateGameWorlds
// (5F5B60) builds as CGameWorld_Siege.
var siegeWorldCodes = map[string]bool{
	"INS_FORT_JA": true, "INS_FORT_DW": true, "INS_FORT_HT": true, "INS_FORT_CT": true,
	"INS_FORT_SK": true, "INS_FORT_BJ": true, "INS_FORT_HM": true, "INS_FORT_ER": true,
}

/*
================
Definition.Siege

A fortress world: the class whose world-manager slot 31 (+0x7C) answers 1,
which sends a teleport through the fortress entry rules (4F2B50).
================
*/
func (d Definition) Siege() bool { return siegeWorldCodes[d.CodeName] }

func Lookup(id DefinitionID) (Definition, bool) {
	for _, row := range shipped {
		if row.ID == id {
			return row, true
		}
	}
	return Definition{}, false
}

func parseDefinitions(data string) ([]Definition, error) {
	var out []Definition
	seen := map[DefinitionID]bool{}
	for index, line := range strings.Split(strings.TrimSuffix(data, "\n"), "\n") {
		fields := strings.Split(strings.TrimSuffix(line, "\r"), "\t")
		if len(fields) != 45 {
			return nil, fmt.Errorf("gameworld row %d: expected 45 fields", index+1)
		}
		var values [4]uint64
		for i, column := range [4]int{0, 2, 3, 4} {
			bits := 16
			if column == 2 {
				bits = 8
			}
			value, err := strconv.ParseUint(fields[column], 10, bits)
			if err != nil {
				return nil, fmt.Errorf("gameworld row %d column %d: %w", index+1, column, err)
			}
			values[i] = value
		}
		row := Definition{ID: DefinitionID(values[0]), CodeName: fields[1], NativeType: uint8(values[1]), LayerLimit: uint16(values[2]), PlayerLimit: uint16(values[3])}
		if row.ID == 0 || row.CodeName == "" || seen[row.ID] {
			return nil, fmt.Errorf("gameworld row %d: invalid or duplicate identity", index+1)
		}
		seen[row.ID] = true
		copy(row.Strings[:], fields[5:25])
		for i, field := range fields[25:] {
			value, err := strconv.ParseInt(field, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("gameworld row %d column %d: %w", index+1, i+25, err)
			}
			row.Numbers[i] = int32(value)
		}
		out = append(out, row)
	}
	return out, nil
}
