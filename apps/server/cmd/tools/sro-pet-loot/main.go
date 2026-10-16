/*
===========================================================================

sro-pet-loot - inspect/set the stopped shard's DB pet equipment factor

Uses the authority store's normal lock and validation; never edits character
records. Shard restart applies the setting.

===========================================================================
*/
package main

import (
	"flag"
	"fmt"
	"log"

	"opensro.online/server/internal/data/store"
)

/*
================
main
================
*/
func main() {
	dir := flag.String("authority-dir", "", "stopped shard authority directory")
	factor := flag.Float64("factor", -1, "0..1 chance per normal equipment drop; omit to inspect")
	flag.Parse()
	if *dir == "" {
		log.Fatal("-authority-dir is required")
	}
	s, err := store.Open(*dir, store.Options{RequireStore: true})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	write := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "factor" {
			write = true
		}
	})
	if write {
		if err := s.SetPetGearDropFactor(*factor); err != nil {
			log.Fatal(err)
		}
	}
	value, err := s.PetGearDropFactor()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("meta.%s=%g (restart shard to apply)\n", store.PetGearDropFactorKey, value)
}
