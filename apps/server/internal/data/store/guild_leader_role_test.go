/*
===========================================================================

guild_leader_role_test.go - the offline master-role repair

A master founded before the port gave the commander role carries role 0.
The authority upgrade gives it role 1, from an older schema and on a
current store alike, touches no other member, and runs only once.

===========================================================================
*/
package store

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
roleMasterGuild

A guild whose master holds role 0 and whose member holds the smith's
role 8, as the port founded guilds before the repair.
================
*/
func roleMasterGuild(t *testing.T, dir string) (master, member int64) {
	t.Helper()
	s := openTest(t, dir, newTestClock())
	leader := guildTestCharacter("rolemaster")
	joiner := guildTestCharacter("rolesmith")
	for _, c := range []*domain.Character{leader, joiner} {
		if err := s.CreateCharacter(testDivision, "account-"+c.Name, c); err != nil {
			t.Fatal(err)
		}
	}
	guildID, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: "Roles", Level: 1},
		domain.GuildMemberRecord{CharID: leader.ID, JID: 1, Name: leader.Name, Grade: 0, PermMask: 0xffffffff}, leader)
	if err != nil {
		t.Fatal(err)
	}
	if _, refusal := s.Guilds().AddGuildMemberAs(testDivision, guildID, leader.ID, 0,
		domain.GuildMemberRecord{CharID: joiner.ID, JID: 2, Name: joiner.Name, Grade: 3, FortressRole: 8}); refusal.Refused() {
		t.Fatalf("join refused: %v", refusal)
	}
	s.Close()
	return leader.ID, joiner.ID
}

/*
================
storedRoles
================
*/
func storedRoles(t *testing.T, dir string) map[int64]uint8 {
	t.Helper()
	s := openTest(t, dir, newTestClock())
	defer s.Close()
	roles := map[int64]uint8{}
	for _, members := range s.guildMembers[testDivision] {
		for _, member := range members {
			roles[member.CharID] = member.FortressRole
		}
	}
	return roles
}

/*
================
TestUpgradeGivesTheMasterTheCommanderRole
================
*/
func TestUpgradeGivesTheMasterTheCommanderRole(t *testing.T) {
	dir := t.TempDir()
	master, smith := roleMasterGuild(t, dir)
	downgradeToLayout7(t, dir)
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preItemForgeVersion)
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	roles := storedRoles(t, dir)
	if roles[master] != domain.GuildFortressRoleCommander || roles[smith] != 8 {
		t.Fatalf("roles after upgrade = %v, want master 1 and smith 8", roles)
	}
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}

/*
================
TestUpgradeRepairsACurrentStoreOnce

A store already at the current schema still upgrades while a master lacks
the role; the repair is the only change, and afterwards it is current.
================
*/
func TestUpgradeRepairsACurrentStoreOnce(t *testing.T) {
	dir := t.TempDir()
	master, smith := roleMasterGuild(t, dir)
	if backup, err := UpgradeAuthority(dir, false); err != nil || backup != "" {
		t.Fatalf("dry run %q: %v", backup, err)
	}
	if roles := storedRoles(t, dir); roles[master] != 0 {
		t.Fatalf("a dry run rewrote the master: %v", roles)
	}
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("repair %q: %v", backup, err)
	}
	roles := storedRoles(t, dir)
	if roles[master] != domain.GuildFortressRoleCommander || roles[smith] != 8 {
		t.Fatalf("roles after repair = %v, want master 1 and smith 8", roles)
	}
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second repair = %v, want ErrAuthorityCurrent", err)
	}
}

/*
================
TestUpgradeRemovesStaleNonmasterCommanders

The previous grant handler allowed role 1 on any member. Repair that
persisted authority even when the master already has the correct role.
================
*/
func TestUpgradeRemovesStaleNonmasterCommanders(t *testing.T) {
	for _, older := range []bool{false, true} {
		name := "current"
		if older {
			name = "older"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			master, smith := roleMasterGuild(t, dir)
			s := openTest(t, dir, newTestClock())
			stale := guildTestCharacter("oldcommand")
			if err := s.CreateCharacter(testDivision, "stale-account", stale); err != nil {
				t.Fatal(err)
			}
			guilds := s.Guilds()
			guildID, _ := guilds.GuildOfCharacter(testDivision, master)
			if _, refusal := guilds.AddGuildMemberAs(testDivision, guildID, master, 0,
				domain.GuildMemberRecord{CharID: stale.ID, JID: 3, Name: stale.Name, Grade: 3, FortressRole: 1}); refusal.Refused() {
				t.Fatalf("stale commander fixture: %v", refusal)
			}
			if !older {
				if _, refusal := guilds.UpdateGuildAs(testDivision, master, "fixture-correct-master", domain.GuildAuthorization{},
					func(g domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
						for i := range members {
							if members[i].CharID == master {
								members[i].FortressRole = domain.GuildFortressRoleCommander
							}
						}
						return g, members, true
					}); refusal.Refused() {
					t.Fatalf("master fixture: %v", refusal)
				}
			}
			s.Close()
			if older {
				downgradeToLayout7(t, dir)
				rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preItemForgeVersion)
			}
			if backup, err := UpgradeAuthority(dir, false); err != nil || backup != "" {
				t.Fatalf("dry run %q: %v", backup, err)
			}
			// Reopening an older schema is refused; inspect its rows directly.
			db, err := connectDB(dir + "/" + DBFileName)
			if err != nil {
				t.Fatal(err)
			}
			var role int
			err = db.QueryRow("SELECT json_extract(record, '$.fortressRole') FROM guild_members WHERE char_id = ?", stale.ID).Scan(&role)
			db.Close()
			if err != nil || role != 1 {
				t.Fatalf("dry run changed stale role: %d, %v", role, err)
			}
			if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
				t.Fatalf("repair %q: %v", backup, err)
			}
			roles := storedRoles(t, dir)
			if roles[master] != 1 || roles[smith] != 8 || roles[stale.ID] != 0 {
				t.Fatalf("repaired roles %v", roles)
			}
			if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
				t.Fatalf("second repair: %v", err)
			}
		})
	}
}
