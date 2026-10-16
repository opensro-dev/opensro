/*
===========================================================================

petaffixes.go - persistent pet blue ranges, selection weights and roll bias

Port-only shard configuration. Seed absent rows without overwriting operator
edits. Read once at startup; invalid settings refuse deployment.
===========================================================================
*/
package store

import (
	"fmt"
	"math"
)

/*
================
PetAffixRule
================
*/
type PetAffixRule struct {
	Code                                uint16
	Name, Group                         string
	Minimum, Maximum, Weight, RollPower float64
}

/*
================
PetAffixRules
================
*/
func (s *Store) PetAffixRules(defaults []PetAffixRule) ([]PetAffixRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS pet_affix_rules (
		code INTEGER PRIMARY KEY, name TEXT NOT NULL, affix_group TEXT NOT NULL,
		minimum REAL NOT NULL, maximum REAL NOT NULL, selection_weight REAL NOT NULL,
		roll_power REAL NOT NULL)`)
	if err != nil {
		return nil, err
	}
	rules := make([]PetAffixRule, 0, len(defaults))
	for _, d := range defaults {
		if _, err = tx.Exec(`INSERT INTO pet_affix_rules VALUES(?,?,?,?,?,?,?) ON CONFLICT(code) DO NOTHING`,
			d.Code, d.Name, d.Group, d.Minimum, d.Maximum, d.Weight, d.RollPower); err != nil {
			return nil, err
		}
		r := d
		if err = tx.QueryRow(`SELECT minimum,maximum,selection_weight,roll_power FROM pet_affix_rules WHERE code=?`, d.Code).
			Scan(&r.Minimum, &r.Maximum, &r.Weight, &r.RollPower); err != nil {
			return nil, err
		}
		for _, v := range []float64{r.Minimum, r.Maximum, r.Weight, r.RollPower} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("pet affix %d: non-finite rule", d.Code)
			}
		}
		if r.Minimum < 0 || r.Maximum < r.Minimum || r.Maximum > 1000000 ||
			math.Trunc(r.Minimum) != r.Minimum || math.Trunc(r.Maximum) != r.Maximum ||
			r.Weight < 0 || r.Weight > 1000000 || r.RollPower < 1 || r.RollPower > 20 {
			return nil, fmt.Errorf("pet affix %d: invalid range, weight or roll power", d.Code)
		}
		rules = append(rules, r)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return rules, nil
}
