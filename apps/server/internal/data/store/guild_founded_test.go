/*
===========================================================================

guild_founded_test.go - the schema 26 step (a guild's founding time)

===========================================================================
*/
package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
TestCreateGuildStampsFoundingTime

The store stamps a new guild with its own clock, whatever the caller put
in the record, and the time survives a reopen.
================
*/
func TestCreateGuildStampsFoundingTime(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	clock.Advance(time.Hour)
	s := openTest(t, dir, clock)
	c := guildTestCharacter("Founder")
	if err := s.CreateCharacter(testDivision, "founder", c); err != nil {
		t.Fatal(err)
	}
	id, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: "Silk", Level: 1, FoundedAtMs: 5},
		domain.GuildMemberRecord{CharID: c.ID, JID: uint32(c.ID), Name: c.Name}, c)
	if err != nil {
		t.Fatal(err)
	}
	want := clock.Now().UnixMilli()
	if guild, _, ok := s.Guilds().Guild(testDivision, id); !ok || guild.FoundedAtMs != want {
		t.Fatalf("founded %d, want %d", guild.FoundedAtMs, want)
	}
	s.Close()
	reopened := openTest(t, dir, clock)
	defer reopened.Close()
	if guild, _, ok := reopened.Guilds().Guild(testDivision, id); !ok || guild.FoundedAtMs != want {
		t.Fatalf("after reopen founded %d, want %d", guild.FoundedAtMs, want)
	}
}

/*
================
TestGuildFoundedUpgradeRewritesNothing

A schema 25 authority upgrades to 26 with its guild row untouched: a guild
founded before the step has no founding time (the site shows "unknown").
A second upgrade has nothing to do.
================
*/
func TestGuildFoundedUpgradeRewritesNothing(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := guildTestCharacter("Elder")
	if err := s.CreateCharacter(testDivision, "elder", c); err != nil {
		t.Fatal(err)
	}
	id, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: "Tea", Level: 1},
		domain.GuildMemberRecord{CharID: c.ID, JID: uint32(c.ID), Name: c.Name}, c)
	if err != nil {
		t.Fatal(err)
	}
	// A schema 25 guild row carries no founding time.
	var record string
	if err := s.db.QueryRow("SELECT record FROM guilds WHERE division = ? AND guild_id = ?", testDivision, id).Scan(&record); err != nil {
		t.Fatal(err)
	}
	before := strings.Replace(record, `,"foundedAtMs":1700000000000`, "", 1)
	if before == record {
		t.Fatalf("the created guild row has no founding time: %s", record)
	}
	if _, err := s.db.Exec("UPDATE guilds SET record = ? WHERE division = ? AND guild_id = ?", before, testDivision, id); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preGuildFoundedVersion)
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	var after string
	if err := reopened.db.QueryRow("SELECT record FROM guilds WHERE division = ? AND guild_id = ?", testDivision, id).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("the upgrade rewrote the guild:\n%s\n%s", before, after)
	}
	if guild, _, ok := reopened.Guilds().Guild(testDivision, id); !ok || guild.FoundedAtMs != 0 {
		t.Fatalf("an old guild reads founded %d, want 0", guild.FoundedAtMs)
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
