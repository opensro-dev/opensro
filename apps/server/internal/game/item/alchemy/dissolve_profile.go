package alchemy

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DissolveProfileFile is the dissolution profile in the projection's
// derived data (gamedata DerivedDir). It is a version adapter, not
// recovered Service=2 data: the user-authorized inference preserves the
// later dump's relative weights for v1.150-published stones. Source
// Service remains 1. It comes from the operator's source-data, never the
// repository; without it dissolution is unavailable.
const DissolveProfileFile = "alchemy/dissolve_v1150.json"

/*
================
loadDissolveProfile

Admit the whole profile or nothing: pools publish only after every row,
degree and element checks out.
================
*/
func (c *Catalog) loadDissolveProfile(data []byte) error {
	var profile struct {
		Policy         string `json:"policy"`
		SourceService  int    `json:"sourceService"`
		SourceSHA256   string `json:"sourceSHA256"`
		EvidenceSHA256 string `json:"evidenceSHA256"`
		Rows           []struct {
			Codename string `json:"codename"`
			Degree   int    `json:"degree"`
			Weight   uint32 `json:"weight"`
		} `json:"rows"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&profile); err != nil {
		return fmt.Errorf("alchemy: dissolution profile: %w", err)
	}
	if profile.Policy != "v1150-inferred-relative-stone-weights-v1" || profile.SourceService != 1 || len(profile.SourceSHA256) != 64 || len(profile.EvidenceSHA256) != 64 || len(profile.Rows) == 0 {
		return fmt.Errorf("alchemy: invalid dissolution profile provenance")
	}
	pools := map[int]DissolvePool{}
	seen := map[string]bool{}
	for _, row := range profile.Rows {
		ref, ok := c.Items[row.Codename]
		if !ok || seen[row.Codename] || row.Degree < 1 || row.Degree > 12 || ref.Degree() != row.Degree || ref.Stack != 1 {
			return fmt.Errorf("alchemy: incompatible dissolution profile item %s", row.Codename)
		}
		seen[row.Codename] = true
		pool := pools[row.Degree]
		choice := WeightedStone{row.Codename, row.Weight}
		switch ref.Flags & 0xfffe {
		case 0x15ec:
			pool.Attribute = append(pool.Attribute, choice)
		case 0x0dec, 0x3dec:
			pool.Magic = append(pool.Magic, choice)
		default:
			return fmt.Errorf("alchemy: non-stone dissolution profile item %s", row.Codename)
		}
		pools[row.Degree] = pool
	}
	for degree, pool := range pools {
		for kind, choices := range [][]WeightedStone{pool.Attribute, pool.Magic} {
			if _, err := c.dissolveWeightTotal(choices, degree, kind); err != nil {
				return err
			}
		}
		for _, kind := range []string{"EARTH", "WATER", "FIRE", "WIND"} {
			name := fmt.Sprintf("ITEM_ETC_ARCHEMY_ELEMENT_%s_%02d", kind, degree)
			ref, ok := c.Items[name]
			if !ok || ref.Flags&0xfffe != 0x2dec || ref.Degree() != degree || ref.Stack == 0 {
				return fmt.Errorf("alchemy: incompatible dissolution element %s", name)
			}
		}
	}
	for _, ref := range c.Items {
		if category(ref.Flags) != "" && ref.Degree() >= 1 && ref.Degree() <= 12 {
			if _, ok := pools[ref.Degree()]; !ok {
				return fmt.Errorf("alchemy: dissolution profile missing degree %d", ref.Degree())
			}
		}
	}
	// Publish only after the whole profile is admitted; never expose partial pools.
	c.DissolveDrops = pools
	return nil
}
