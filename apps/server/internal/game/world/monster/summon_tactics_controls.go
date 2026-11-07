/*
===========================================================================

summon_tactics_controls.go - full tactics rows for summoned monsters

The reference server's summon factory hands every generated monster one
whole RefTactics row from its reference's default list (summon_tactics.go).
The v1.188 summon evidence keeps only three of that row's columns (sight,
flags, target policy), so a summoned child had no TacticsControls: no
AggressType, help rules, trace or homing data. The v1.150 tactics rows the
nest population already uses (tactics_controls.json) carry the whole row.
Each evidence row is joined to the v1.150 row of the same reference whose
three columns match exactly; a reference whose list does not match row for
row keeps its evidence-only rows.

===========================================================================
*/

package monster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

/*
================
loadTacticsPolicies

The tactics controls document's Policies by reference: every RefTactics row
whose ObjectID is a reference some anchor names, keyed by that anchor's
codename. The default list holds every row of the reference, not only the
rows a nest anchors.
================
*/
func loadTacticsPolicies(data []byte) map[string][]TacticsControls {
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
		panic(fmt.Errorf("tactics policies: %w", err))
	}
	objectOf := map[string]uint32{}
	for _, row := range doc.Anchors {
		var code string
		if err := json.Unmarshal(row.Key[0], &code); err != nil {
			panic(err)
		}
		if policy, ok := doc.Policies[row.Normal]; ok {
			objectOf[code] = policy.ObjectID
		}
	}
	byObject := map[uint32][]TacticsControls{}
	for _, policy := range doc.Policies {
		byObject[policy.ObjectID] = append(byObject[policy.ObjectID], policy)
	}
	out := make(map[string][]TacticsControls, len(objectOf))
	for code, object := range objectOf {
		rows := byObject[object]
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		out[code] = rows
	}
	return out
}

/*
================
attachSummonTacticsControls

Gives each evidence row the whole v1.150 row it names. The default list is
partitioned as the reference server partitions it: the normal list (group 0)
holds the rows with a ChampionTacticsID, the other list the rows without one.
Only a list that matches row for row on sight, flags and target policy is
joined; one ambiguous or missing row leaves the whole reference as it was.
================
*/
func attachSummonTacticsControls(out map[string][2][]SummonTactics, policies map[string][]TacticsControls) {
	for code, groups := range out {
		candidates, ok := policies[code]
		if !ok {
			continue
		}
		joined := groups
		matched := true
		for group := range groups {
			var list []TacticsControls
			for _, c := range candidates {
				if (c.ChampionID != 0) == (group == 0) {
					list = append(list, c)
				}
			}
			if len(list) != len(groups[group]) {
				matched = false
				break
			}
			used := make([]bool, len(list))
			rows := make([]SummonTactics, len(groups[group]))
			for i, row := range groups[group] {
				found := -1
				for j, c := range list {
					if used[j] || float64(c.SightRange) != row.SightRange || c.Flags != row.NativeFlags || c.ChangeTarget != row.TargetPolicy {
						continue
					}
					if found >= 0 {
						// Two rows answer one evidence row: the join would guess.
						found = -2
						break
					}
					found = j
				}
				if found < 0 {
					matched = false
					break
				}
				used[found] = true
				row.Controls, row.HasControls = list[found], true
				rows[i] = row
			}
			if !matched {
				break
			}
			joined[group] = rows
		}
		if matched {
			out[code] = joined
		}
	}
}
