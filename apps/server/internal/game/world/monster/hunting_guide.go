/*
===========================================================================

hunting_guide.go - the immutable, public beginner hunting atlas

Port-only, not native. This projection describes approximate ordinary outdoor
locations; it never reads live populations, timers or player state.

===========================================================================
*/

package monster

import (
	"math"
	"sort"
)

/*
================
HuntingGuidePoint

Only the outdoor position needed by M; no height, radius or live identity.
================
*/
type HuntingGuidePoint struct {
	RegionID uint16  `json:"regionId"`
	X        float32 `json:"x"`
	Z        float32 `json:"z"`
}

/*
================
HuntingGuideEntry
================
*/
type HuntingGuideEntry struct {
	RefObjID uint32              `json:"refObjId"`
	NameKey  string              `json:"nameKey"`
	Name     string              `json:"name"`
	Level    uint8               `json:"level"`
	Points   []HuntingGuidePoint `json:"points"`
}

/*
================
HuntingGuide

Owner-approved port addition. The effective template supplies locations and
disabled caps. Infer a conservative beginner scope: ordinary default-world
retail anchors only, excluding resource-authored event/development areas.
No bounds are invented from roaming, home radii or current creature positions.
================
*/
func (t Template) HuntingGuide() []HuntingGuideEntry {
	entries := make(map[uint32]*HuntingGuideEntry)
	seen := make(map[uint32]map[HuntingGuidePoint]bool)
	for _, nest := range t.Nests {
		ref, ok := t.Refs[nest.RefObjID]
		if !ok || ref.Structure || ref.TypeID4 != 1 || ref.MonsterType != 0 || ref.Level == 0 ||
			ref.Name == "" || ref.Name == "xxx" || nest.RegionID&0x8000 != 0 ||
			nest.WorldCode != "" && nest.WorldCode != "INS_DEFAULT" || nest.EventStructID != 0 ||
			nest.StartVacant || nest.InstanceLimit() == 0 ||
			nest.PolicyPinned && !nest.RetailEvidence {
			continue
		}
		point := HuntingGuidePoint{RegionID: nest.RegionID, X: float32(nest.X), Z: float32(nest.Z)}
		if math.IsNaN(float64(point.X)) || math.IsInf(float64(point.X), 0) ||
			math.IsNaN(float64(point.Z)) || math.IsInf(float64(point.Z), 0) {
			continue
		}
		entry := entries[ref.RefObjID]
		if entry == nil {
			entry = &HuntingGuideEntry{RefObjID: ref.RefObjID, NameKey: ref.NameStrID, Name: ref.Name, Level: ref.Level}
			entries[ref.RefObjID] = entry
			seen[ref.RefObjID] = make(map[HuntingGuidePoint]bool)
		}
		if !seen[ref.RefObjID][point] {
			entry.Points = append(entry.Points, point)
			seen[ref.RefObjID][point] = true
		}
	}
	result := make([]HuntingGuideEntry, 0, len(entries))
	for _, entry := range entries {
		sort.Slice(entry.Points, func(i, j int) bool {
			a, b := entry.Points[i], entry.Points[j]
			if a.RegionID != b.RegionID {
				return a.RegionID < b.RegionID
			}
			if a.X != b.X {
				return a.X < b.X
			}
			return a.Z < b.Z
		})
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RefObjID < result[j].RefObjID })
	return result
}
