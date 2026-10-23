package monster

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// TacticsControls preserves the complete version-owned numeric source row.
// Presence is explicit: zero is authored data, never a request for defaults.
// Source IDs identify evidence only; runtime entity/reference IDs remain v1.150.
// Extending the publisher requires extending this strict decoding contract.
type TacticsControls struct {
	ID, ObjectID                                         uint32
	AIQoS                                                uint8
	MaxStamina                                           int32
	StaminaVariance                                      uint8
	SightRange                                           int32
	AggressType                                          uint8
	AggressData                                          int32
	ChangeTarget, HelpRequest, HelpResponse, BattleStyle uint8
	BattleStyleData                                      int32
	DiversionBasis                                       uint8
	DiversionBasisData                                   [8]int32
	DiversionKeepBasis                                   uint8
	DiversionKeepBasisData                               [8]int32
	KeepDistance                                         uint8
	KeepDistanceData                                     int32
	TraceType, TraceBoundary                             uint8
	TraceData                                            int32
	HomingType                                           uint8
	HomingData                                           int32
	AggressOnHoming, FleeType                            uint8
	ChampionID, Flags                                    uint32
	// A later source column, separate from ChangeTarget at native +0x20.
	AggroType    uint8
	HasAggroType bool
}

type tacticsControlPair struct {
	Normal, Champion TacticsControls
	HasChampion      bool
}

func loadTacticsControls(data []byte) map[populationEvidenceKey]tacticsControlPair {
	var doc struct {
		Sources  map[string]string
		Policies map[string]TacticsControls
		Anchors  []struct {
			Key              [5]json.RawMessage
			Normal, Champion string
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		panic(fmt.Errorf("tactics controls: %w", err))
	}
	result := make(map[populationEvidenceKey]tacticsControlPair, len(doc.Anchors))
	for _, row := range doc.Anchors {
		var code string
		var numbers [4]int
		if err := json.Unmarshal(row.Key[0], &code); err != nil {
			panic(err)
		}
		for i := range numbers {
			if err := json.Unmarshal(row.Key[i+1], &numbers[i]); err != nil {
				panic(err)
			}
		}
		key := populationEvidenceKey{Codename: code, RegionID: uint16(numbers[0]), X10: int64(numbers[1]), Y10: int64(numbers[2]), Z10: int64(numbers[3])}
		normal, ok := doc.Policies[row.Normal]
		if !ok {
			panic("missing normal tactics: " + row.Normal)
		}
		pair := tacticsControlPair{Normal: normal}
		if row.Champion != "" {
			champion, ok := doc.Policies[row.Champion]
			if !ok {
				panic("missing champion tactics: " + row.Champion)
			}
			pair.Champion, pair.HasChampion = champion, true
		}
		if _, exists := result[key]; exists {
			panic("duplicate tactics anchor: " + code)
		}
		result[key] = pair
	}
	return result
}
