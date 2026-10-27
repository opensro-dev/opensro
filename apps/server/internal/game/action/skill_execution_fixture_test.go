/*
===========================================================================

skill_execution_fixture_test.go - one authored row per execution family

The imported-tier coverage proof used to live only in an opt-in
inventory that needs the real tables, so a shape parser quietly
un-claiming a family would go unnoticed. These eight authored
118-column rows - one per execution family, shapes mirroring shipped
rows under synthetic identities - run through the production loader
and the press router with no game data present: if a family loses its
owner, exactly its assertion dies.

===========================================================================
*/

package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// fixtureSkillRows is one authored 118-column row per execution
// family: offense, timed, passive, instant, recovery, threat, and a
// two-stage chain (root and stage).
var fixtureSkillRows = []string{
	`1	9201	174	SYN_FIXTURE_OFFENSE	SYN_FIXTURE_OFFENSE	SKILL_CH_SWORD_SMASH_A	0	1	2	0	99999999	0	411	1022	3000	0	0	0	33554432	1	0	0	1	1	0	0	0	0	0	1	1	0	0	0	257	0	5	0	0	0	0	0	0	0	0	0	2	0	0	0	2	3	0	19	0	0	0	0	0	0	0	skill\china\sword_smash_a.ddj	SN_SKILL_CH_SWORD_SMASH_A	xxx	SN_SKILL_CH_SWORD_SMASH_A_TT_DESC	SN_SKILL_CH_SWORD_SMASH_A_STUDY	0	0	0	6386804	5	143	15	18	143	1734702198	1296122196	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0`,
	`1	9202	457	SYN_FIXTURE_TIMED	SYN_FIXTURE_TIMED	SKILL_EU_ROG_STEALTHA_CHANGE_A	0	1	2	0	99999999	0	0	500	10000	0	0	0	0	0	0	150	1	1	0	0	0	0	0	0	1	0	0	0	515	0	15	0	0	0	0	0	0	0	0	0	24	1	0	0	13	12	0	27	0	0	0	0	1	1	0	skill\europe\rog_stealtha_change_a.ddj	SN_SKILL_EU_ROG_STEALTHA_CHANGE_A	xxx	SN_SKILL_EU_ROG_STEALTHA_CHANGE_A_TT_DESC	SN_SKILL_EU_ROG_STEALTHA_CHANGE_A_STUDY	0	69	3	1851946342	1650619750	1685418593	3000	1751741549	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0`,
	`1	9203	422	SYN_FIXTURE_PASSIVE	SYN_FIXTURE_PASSIVE	SKILL_EU_WARRIOR_SHIELDP_ATTACK_A	0	1	0	0	99999999	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	513	0	4	0	0	0	0	0	0	0	0	0	2	1	0	0	255	255	0	0	0	0	0	0	0	2	0	skill\europe\warrior_shieldp_attack_a.ddj	SN_SKILL_EU_WARRIOR_SHIELDP_ATTACK_A	xxx	SN_SKILL_EU_WARRIOR_SHIELDP_ATTACK_A_TT_DESC	SN_SKILL_EU_WARRIOR_SHIELDP_ATTACK_A_STUDY	0	78	4	1936028790	1160860481	4	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0`,
	`1	9204	271	SYN_FIXTURE_INSTANT	SYN_FIXTURE_INSTANT	SKILL_CH_FIRE_GIGONGTA_A	0	1	1	0	99999999	0	0	0	5000	0	0	0	1	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	275	0	5	0	0	0	0	0	0	0	0	0	2	0	0	0	255	255	0	52	0	0	0	1	2	0	0	skill\china\fire_gigongta_a.ddj	SN_SKILL_CH_FIRE_GIGONGTA_A	xxx	SN_SKILL_CH_FIRE_GIGONGTA_A_TT_DESC	SN_SKILL_CH_FIRE_GIGONGTA_A_STUDY	0	0	3	1685418593	5000	6386804	8	100	16	26	100	25205	30	25	1	1734702198	1296122196	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0`,
	`1	9205	645	SYN_FIXTURE_RECOVERY	SYN_FIXTURE_RECOVERY	SKILL_EU_CLERIC_HEALA_GROUP_A	0	1	2	0	99999999	500	500	1166	5000	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	518	0	20	0	0	0	642	0	0	2	0	0	53	1	0	0	15	255	0	197	0	0	0	2	1	4	0	skill\europe\cleric_heala_group_a.ddj	SN_SKILL_EU_CLERIC_HEALA_GROUP_A	xxx	SN_SKILL_EU_CLERIC_HEALA_GROUP_A_TT_DESC	SN_SKILL_EU_CLERIC_HEALA_GROUP_A_STUDY	0	72	0	6645362	1	1	250	8	0	5	1751474540	246	0	0	0	1836542056	150	1734702198	1212961365	1734702198	1212960068	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0`,
	`1	9206	409	SYN_FIXTURE_THREAT	SYN_FIXTURE_THREAT	SKILL_EU_WARRIOR_FRENZYA_TOUNT_A	0	1	2	0	99999999	0	0	500	5000	0	0	0	0	0	0	150	1	1	0	0	0	0	0	1	1	0	0	0	513	0	10	0	0	0	0	0	0	0	0	0	9	1	0	0	255	255	0	11	0	0	0	0	0	0	0	skill\europe\warrior_frenzya_tount_a.ddj	SN_SKILL_EU_WARRIOR_FRENZYA_TOUNT_A	xxx	SN_SKILL_EU_WARRIOR_FRENZYA_TOUNT_A_TT_DESC	SN_SKILL_EU_WARRIOR_FRENZYA_TOUNT_A_STUDY	0	78	0	6645362	1	2	50	3	0	16	1953395762	1016	0	1886876788	2000	1919250793	6	7	1919250793	6	8	1919250793	6	9	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0`,
	`1	9207	428	SYN_FIXTURE_CHAIN_ROOT	SYN_FIXTURE_CHAIN_ROOT	SKILL_EU_WARRIOR_ONEHANDA_SHIELD_B	0	1	2	9208	99999999	0	0	1056	4000	0	0	0	16777216	0	0	0	1	1	0	0	0	0	0	1	1	0	0	0	513	0	60	0	0	0	422	427	0	15	12	0	726	1	0	0	7	255	0	425	0	0	0	0	0	3	4	skill\europe\warrior_onehanda_shield_b.ddj	SN_SKILL_EU_WARRIOR_ONEHANDA_SHIELD_B	xxx	SN_SKILL_EU_WARRIOR_ONEHANDA_SHIELD_B_TT_DESC	SN_SKILL_EU_WARRIOR_ONEHANDA_SHIELD_B_STUDY	0	80	0	6386804	5	97	364	445	0	28003	2	1	6645362	1	2	20	3	35	24	29548	20000	25	6	1734702198	1160860481	1953395762	1746	150	1919250793	4	2	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0`,
	`1	9208	428	SYN_FIXTURE_CHAIN_STAGE	SYN_FIXTURE_CHAIN_STAGE	SKILL_EU_WARRIOR_ONEHANDA_SHIELD_B	0	1	2	0	99999999	0	0	944	0	0	0	0	0	0	0	0	1	1	0	0	0	0	0	1	1	0	0	0	513	0	60	0	0	0	422	427	0	15	12	0	0	1	0	0	7	255	0	0	0	0	0	255	255	255	255	skill\europe\warrior_onehanda_shield_b.ddj	SN_SKILL_EU_WARRIOR_ONEHANDA_SHIELD_B	xxx	SN_SKILL_EU_WARRIOR_ONEHANDA_SHIELD_B_TT_DESC	SN_SKILL_EU_WARRIOR_ONEHANDA_SHIELD_B_STUDY	0	80	0	6386804	5	87	364	445	0	28003	2	1	6645362	1	2	20	3	35	24	27490	80	80	29548	20000	25	6	1734702198	1160860481	1953395762	1561	150	1919250793	4	2	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0	0`,
}

/*
================
loadFixtureSkills
================
*/
func loadFixtureSkills(t *testing.T) *enterworld.TextdataSkills {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skilldata.txt"), []byte("shard_a.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shard := strings.Join(fixtureSkillRows, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "shard_a.txt"), []byte(shard), 0o644); err != nil {
		t.Fatal(err)
	}
	source := enterworld.NewTextdataSkills(dir)
	if err := source.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if source.Len() != len(fixtureSkillRows) {
		t.Fatalf("loaded %d rows, want %d authored", source.Len(), len(fixtureSkillRows))
	}
	return source
}

/*
================
TestExecutionFamiliesStayClaimed

Every family keeps its loader pin and its press owner. The passive
and the chain stage are the two deliberate negatives: a passive is
never pressed, and a stage is only reached through its root, so both
must stay unhandled while their pins hold.
================
*/
func TestExecutionFamiliesStayClaimed(t *testing.T) {
	source := loadFixtureSkills(t)

	row := func(id uint32) enterworld.SkillRow {
		t.Helper()
		r, ok := source.SkillByID(id)
		if !ok {
			t.Fatalf("fixture row %d did not load", id)
		}
		return r
	}

	offense := row(9201)
	if !offense.DirectOffensePinned {
		t.Errorf("offense: not pinned as direct offense: %+v", offense.Attack)
	}
	if routeSkillCommand(offense) != skillRouteOffensive || !SkillPressHandled(source, offense) {
		t.Errorf("offense: route %v handled %v, want offensive/handled", routeSkillCommand(offense), SkillPressHandled(source, offense))
	}

	timed := row(9202)
	if !timed.TimedEffect.Pinned || !timed.TimedEffect.Targeted {
		t.Errorf("timed: not pinned as a targeted timed effect: %+v", timed.TimedEffect)
	}
	if routeSkillCommand(timed) != skillRouteTimedTarget || !SkillPressHandled(source, timed) {
		t.Errorf("timed: route %v handled %v, want timed-target/handled", routeSkillCommand(timed), SkillPressHandled(source, timed))
	}

	passive := row(9203)
	if !passive.PassiveParameters.Pinned && !passive.PassiveDefense.Pinned && !passive.PassiveCritical.Pinned {
		t.Errorf("passive: no passive pin held: %+v", passive)
	}
	if SkillPressHandled(source, passive) {
		t.Errorf("passive: a passive must stay unhandled by the press router")
	}

	instant := row(9204)
	if !instant.Imbue.Pinned {
		t.Errorf("instant: imbue not pinned: %+v", instant.Imbue)
	}
	if !SkillPressHandled(source, instant) {
		t.Errorf("instant: the imbue press must be handled before routing")
	}

	recovery := row(9205)
	if !recovery.Recovery.PartyHealPinned {
		t.Errorf("recovery: group heal not pinned: %+v", recovery.Recovery)
	}
	if routeSkillCommand(recovery) != skillRouteSupport || !SkillPressHandled(source, recovery) {
		t.Errorf("recovery: route %v handled %v, want support/handled", routeSkillCommand(recovery), SkillPressHandled(source, recovery))
	}

	threat := row(9206)
	if !threat.Threat.Only {
		t.Errorf("threat: taunt not pinned as threat-only: %+v", threat.Threat)
	}
	if !SkillPressHandled(source, threat) {
		t.Errorf("threat: the taunt press must be handled")
	}

	root := row(9207)
	if root.ChainNext != 9208 {
		t.Errorf("chain: root links to %d, want its stage 9208", root.ChainNext)
	}
	if sequence, ok := enterworld.OffensiveSequence(source, root.ID); !ok || len(sequence) != 2 {
		t.Errorf("chain: root sequence %v/%v, want two stages", sequence, ok)
	}
	if !SkillPressHandled(source, root) {
		t.Errorf("chain: the root press must be handled")
	}

	stage := row(9208)
	if !stage.ChainSub {
		t.Errorf("chain: stage not marked a sub-row")
	}
	if SkillPressHandled(source, stage) {
		t.Errorf("chain: a stage must stay unhandled as a root press")
	}
}
