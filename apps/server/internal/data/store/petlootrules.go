/*
===========================================================================

petlootrules.go - DB-owned pet equipment chance relative to normal loot

Port-only. Optional meta key, no character/layout migration. Read at shard
startup; the operator changes the database setting while the shard is stopped.

===========================================================================
*/
package store

import (
	"fmt"
	"math"
	"strconv"
)

const PetGearDropFactorKey = "petGearDropFactor"
const DefaultPetGearDropFactor = 0.10

/*
================
validPetGearDropFactor
================
*/
func validPetGearDropFactor(factor float64) bool {
	return !math.IsNaN(factor) && !math.IsInf(factor, 0) && factor >= 0 && factor <= 1
}

/*
================
PetGearDropFactor
================
*/
func (s *Store) PetGearDropFactor() (float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	text, present, err := readMeta(s.db, PetGearDropFactorKey)
	if err != nil {
		return 0, err
	}
	if !present {
		return DefaultPetGearDropFactor, nil
	}
	factor, err := strconv.ParseFloat(text, 64)
	if err != nil || !validPetGearDropFactor(factor) {
		return 0, fmt.Errorf("%s must be a finite number from 0 to 1: %q", PetGearDropFactorKey, text)
	}
	return factor, nil
}

/*
================
SetPetGearDropFactor
================
*/
func (s *Store) SetPetGearDropFactor(factor float64) error {
	if !validPetGearDropFactor(factor) {
		return fmt.Errorf("%s must be finite and from 0 to 1", PetGearDropFactorKey)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", PetGearDropFactorKey, strconv.FormatFloat(factor, 'g', -1, 64))
	return err
}
