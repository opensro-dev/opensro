/*
===========================================================================

cossummoner.go - persistent summoner item bodies

492D20/492D40 distinguish absent, summoned, dormant and dead records. The
item subtype selects attack or pickup companions; pickup records add rental
time. Statistics remain server authority and never come from this wire view.

===========================================================================
*/
package wire

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"unicode/utf8"
)

const (
	cosAttackSummonerSubtype = 1
	cosPickupSummonerSubtype = 2
	maxCosRentalJobs         = 255
	maxCosWireNameBytes      = 65535
)

/*
================
encodeCosSummoner
================
*/
func encodeCosSummoner(flags uint16, pet *domain.CharacterCOS) ([]byte, error) {
	w := NewWriter(16).U8(domain.COSItemState(pet))
	if pet == nil {
		return w.Payload(), nil
	}
	subtype := flags >> 11
	if subtype != cosAttackSummonerSubtype && subtype != cosPickupSummonerSubtype {
		return nil, fmt.Errorf("unsupported persistent summoner subtype %d", subtype)
	}
	if pet.RefObjID == 0 || len(pet.Name) > maxCosWireNameBytes || !utf8.ValidString(pet.Name) || len(pet.Rentals) > maxCosRentalJobs {
		return nil, fmt.Errorf("invalid persistent summoner record")
	}
	w.U32(pet.RefObjID).U16(uint16(len(pet.Name))).Bytes([]byte(pet.Name))
	if subtype == cosPickupSummonerSubtype {
		w.U32(uint32(pet.RentalRemainingSeconds))
	}
	w.U8(uint8(len(pet.Rentals)))
	for _, job := range pet.Rentals {
		if job.Kind != 0 && job.Kind != 5 {
			return nil, fmt.Errorf("unsupported COS rental kind %d", job.Kind)
		}
		w.U8(job.Kind).U32(job.ID).U32(uint32(job.RemainingSeconds))
		if job.Kind == 5 {
			w.U32(job.Tag).U8(job.Flag)
		}
	}
	return w.Payload(), nil
}

/*
================
readCosSummoner

Reject unknown states and job variants before accepting any partial record.
The item subtype and its associated character family are checked at summon
admission against the reference catalogue, never chosen by the client.
================
*/
func readCosSummoner(r *Reader, flags uint16) (*domain.CharacterCOS, error) {
	state, err := r.U8()
	if err != nil {
		return nil, err
	}
	if state == cosSummonerNoRecord {
		return nil, nil
	}
	if state < 2 || state > 4 {
		return nil, fmt.Errorf("invalid COS item state %d", state)
	}
	subtype := flags >> 11
	if subtype != cosAttackSummonerSubtype && subtype != cosPickupSummonerSubtype {
		return nil, fmt.Errorf("unsupported persistent summoner subtype %d", subtype)
	}
	pet := &domain.CharacterCOS{Summoned: state == 2}
	if state != 4 {
		pet.StateFlags = 1
	}
	if pet.Summoned {
		pet.StateFlags |= 2
	}
	if pet.RefObjID, err = r.U32(); err != nil {
		return nil, err
	}
	if pet.RefObjID == 0 {
		return nil, fmt.Errorf("empty COS reference")
	}
	size, err := r.U16()
	if err != nil {
		return nil, err
	}
	name, err := r.Bytes(int(size))
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(name) {
		return nil, fmt.Errorf("invalid COS name encoding")
	}
	pet.Name = string(name)
	if subtype == cosPickupSummonerSubtype {
		seconds, e := r.U32()
		if e != nil {
			return nil, e
		}
		pet.RentalRemainingSeconds = int32(seconds)
	}
	count, err := r.U8()
	if err != nil {
		return nil, err
	}
	for range count {
		var job domain.COSRental
		if job.Kind, err = r.U8(); err != nil {
			return nil, err
		}
		if job.Kind != 0 && job.Kind != 5 {
			return nil, fmt.Errorf("unsupported COS rental kind %d", job.Kind)
		}
		if job.ID, err = r.U32(); err != nil {
			return nil, err
		}
		seconds, e := r.U32()
		if e != nil {
			return nil, e
		}
		job.RemainingSeconds = int32(seconds)
		if job.Kind == 5 {
			if job.Tag, err = r.U32(); err != nil {
				return nil, err
			}
			if job.Flag, err = r.U8(); err != nil {
				return nil, err
			}
		}
		pet.Rentals = append(pet.Rentals, job)
	}
	return pet, nil
}

/*
================
ValidateCOSItem

Authority boundaries call this before committing a retained item. Encoders
cannot replace an invalid record with the no-record byte without data loss.
================
*/
func ValidateCOSItem(flags uint16, pet *domain.CharacterCOS) error {
	if !IsCosSummoner(flags) && pet != nil {
		return fmt.Errorf("companion record on a non-summoner item")
	}
	if !IsCosSummoner(flags) {
		return nil
	}
	_, err := encodeCosSummoner(flags, pet)
	return err
}
