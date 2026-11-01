/*
===========================================================================

trainingcamps_test.go - the training-camp door: opening, admission,
notice authority and persistence

===========================================================================
*/
package store

import (
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
openTestCamp

The master's camp opened at the NPC with no fee and the lowest level floor,
so a test reaches admission without staging gold or levels.
================
*/
func openTestCamp(t *testing.T, s *Store, master *domain.Character) domain.TrainingCampRecord {
	t.Helper()
	opened, refusal := s.TrainingCamps().OpenCamp(testDivision, domain.TrainingCampOpening{MasterCharID: master.ID, MinLevel: 1})
	if refusal != domain.TrainingCampOpenAccepted {
		t.Fatalf("open camp refused: %d", refusal)
	}
	return opened.Camp
}

/*
================
TestTrainingCampNoticeAuthorityAndPersistence
================
*/
func TestTrainingCampNoticeAuthorityAndPersistence(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	master, student, outsider := guildTestCharacter("noticemaster"), guildTestCharacter("notestudent"), guildTestCharacter("noteoutsider")
	for _, c := range []*domain.Character{master, student, outsider} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}
	opened := openTestCamp(t, s, master)
	a, ok := s.TrainingCamps().AdmitStudent(testDivision, domain.TrainingCampAdmission{MasterCharID: master.ID, StudentCharID: student.ID, ExpectedCampID: opened.ID, MasterMinLevel: 1, StudentMaxLevel: 140, StudentLimit: 5})
	if !ok {
		t.Fatal("admission failed")
	}
	for _, request := range []struct {
		division     string
		author, camp int64
	}{{testDivision, student.ID, a.Camp.ID}, {testDivision, outsider.ID, a.Camp.ID}, {testDivision, master.ID, a.Camp.ID + 1}, {"other", master.ID, a.Camp.ID}} {
		if _, _, ok := s.TrainingCamps().UpdateNotice(request.division, request.author, request.camp, "unauthorized", "text"); ok {
			t.Fatalf("accepted %+v", request)
		}
	}
	unchanged, _, _ := s.TrainingCamps().Camp(testDivision, a.Camp.ID)
	if unchanged != a.Camp {
		t.Fatal("refusals changed camp")
	}
	camp, members, ok := s.TrainingCamps().UpdateNotice(testDivision, master.ID, a.Camp.ID, "Welcome", "Meet at the gate.\nRésumé")
	if !ok || camp.Subject != "Welcome" || camp.Contents != "Meet at the gate.\nRésumé" || !reflect.DeepEqual(members, a.Members) {
		t.Fatalf("update: %+v %+v %v", camp, members, ok)
	}
	members[0].Kind = 2
	_, live, _ := s.TrainingCamps().Camp(testDivision, a.Camp.ID)
	if !reflect.DeepEqual(live, a.Members) {
		t.Fatal("returned members alias authority")
	}
	s.MutateCharacter(master, "test-delete-pending", func() { master.DeletePending = true })
	if _, _, ok := s.TrainingCamps().UpdateNotice(testDivision, master.ID, a.Camp.ID, "bad", "bad"); ok {
		t.Fatal("delete-pending author accepted")
	}
	s.MutateCharacter(master, "test-restore", func() { master.DeletePending = false })
	s.MutateCharacter(master, "test-notice-death-race", func() { hp := int64(0); master.CurrentHP = &hp })
	if _, _, ok := s.TrainingCamps().UpdateNotice(testDivision, master.ID, a.Camp.ID, "bad", "bad"); ok {
		t.Fatal("dead author committed a notice")
	}
	s.MutateCharacter(master, "test-notice-revive", func() { master.CurrentHP = nil })
	s.Close()
	reopened := openTest(t, dir, clock)
	stored, storedMembers, ok := reopened.TrainingCamps().Camp(testDivision, a.Camp.ID)
	if !ok || stored != camp || !reflect.DeepEqual(storedMembers, a.Members) {
		t.Fatalf("reopened: %+v %+v %v", stored, storedMembers, ok)
	}
}

/*
================
TestTrainingCampDeletionInvariant
================
*/
func TestTrainingCampDeletionInvariant(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	master := guildTestCharacter("campmaster")
	student := guildTestCharacter("campstudent")
	outsider := guildTestCharacter("campoutside")
	for _, c := range []*domain.Character{master, student, outsider} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}
	opened := openTestCamp(t, s, master)
	admission, ok := s.TrainingCamps().AdmitStudent(testDivision, domain.TrainingCampAdmission{
		MasterCharID:    master.ID,
		StudentCharID:   student.ID,
		ExpectedCampID:  opened.ID,
		MasterMinLevel:  1,
		StudentMaxLevel: 140,
		StudentLimit:    5,
	})
	if !ok {
		t.Fatal("initial student admission refused")
	}
	camp := admission.Camp

	stamp := clock.Now().UTC().Format(time.RFC3339)
	if s.ReserveCharacterDeletion(student, stamp) {
		t.Fatal("training-camp member entered delete-pending state")
	}
	if student.DeletePending {
		t.Fatal("refused reservation mutated the student")
	}
	if !s.ReserveCharacterDeletion(outsider, stamp) {
		t.Fatal("guildless/campless character reservation refused")
	}
	if _, admitted := s.TrainingCamps().AdmitStudent(testDivision, domain.TrainingCampAdmission{
		MasterCharID:    master.ID,
		StudentCharID:   outsider.ID,
		ExpectedCampID:  camp.ID,
		MasterMinLevel:  1,
		StudentMaxLevel: 140,
		StudentLimit:    5,
	}); admitted {
		t.Fatal("delete-pending character joined a training camp")
	}
	s.MutateCharacter(outsider, "delete-restore", func() {
		outsider.DeletePending = false
		outsider.DeleteReservedAt = ""
	})

	// Simulate an impossible pre-invariant record and prove the reaper does
	// not turn it into a dangling MasterCharID.
	s.MutateCharacter(master, "invalid-delete-reserve", func() {
		master.DeletePending = true
		master.DeleteReservedAt = stamp
	})
	clock.Advance(DeleteReservationWindow + time.Hour)
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 0 {
		t.Fatalf("reaped training-camp master(s) %v, want fail-closed none", reaped)
	}
	if gotCamp, gotMembers, ok := s.TrainingCamps().Camp(testDivision, camp.ID); !ok ||
		gotCamp.MasterCharID != master.ID || len(gotMembers) != 2 {
		t.Fatalf("camp after refused reap = %+v members=%+v ok=%v", gotCamp, gotMembers, ok)
	}
	s.MutateCharacter(master, "restore-impossible-reservation", func() {
		master.DeletePending = false
		master.DeleteReservedAt = ""
	})
	s.Close()

	s2 := openTest(t, dir, clock)
	if gotCamp, gotMembers, ok := s2.TrainingCamps().Camp(testDivision, camp.ID); !ok ||
		gotCamp.MasterCharID != master.ID || len(gotMembers) != 2 {
		t.Fatalf("reopened camp = %+v members=%+v ok=%v", gotCamp, gotMembers, ok)
	}
	if got := len(s2.Characters().CharactersForDivision(testDivision)); got != 3 {
		t.Fatalf("reopened live characters = %d, want 3", got)
	}
}

/*
================
TestTrainingCampAdmissionRefusesInvalidAggregateCommands
================
*/
func TestTrainingCampAdmissionRefusesInvalidAggregateCommands(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	master := guildTestCharacter("campvalid")
	student := guildTestCharacter("studentvalid")
	for _, character := range []*domain.Character{master, student} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name      string
		admission domain.TrainingCampAdmission
	}{
		{"unknown student", domain.TrainingCampAdmission{MasterCharID: master.ID, StudentCharID: 999, MasterMinLevel: 1, StudentMaxLevel: 140, StudentLimit: 5}},
		{"self admission", domain.TrainingCampAdmission{MasterCharID: master.ID, StudentCharID: master.ID, MasterMinLevel: 1, StudentMaxLevel: 140, StudentLimit: 5}},
		{"invalid master floor", domain.TrainingCampAdmission{MasterCharID: master.ID, StudentCharID: student.ID, MasterMinLevel: 0, StudentMaxLevel: 140, StudentLimit: 5}},
		{"invalid student ceiling", domain.TrainingCampAdmission{MasterCharID: master.ID, StudentCharID: student.ID, MasterMinLevel: 1, StudentMaxLevel: 0, StudentLimit: 5}},
		{"invalid student limit", domain.TrainingCampAdmission{MasterCharID: master.ID, StudentCharID: student.ID, MasterMinLevel: 1, StudentMaxLevel: 140, StudentLimit: 8}},
		{"stale existing camp", domain.TrainingCampAdmission{MasterCharID: master.ID, StudentCharID: student.ID, ExpectedCampID: master.ID + 99, MasterMinLevel: 1, StudentMaxLevel: 140, StudentLimit: 5}},
		// An admission never creates a camp: only OpenCamp does (0x72EC).
		{"campless master", domain.TrainingCampAdmission{MasterCharID: master.ID, StudentCharID: student.ID, MasterMinLevel: 1, StudentMaxLevel: 140, StudentLimit: 5}},
		{"campless master naming its own id", domain.TrainingCampAdmission{MasterCharID: master.ID, StudentCharID: student.ID, ExpectedCampID: master.ID, MasterMinLevel: 1, StudentMaxLevel: 140, StudentLimit: 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := s.TrainingCamps().AdmitStudent(testDivision, tc.admission); ok {
				t.Fatal("invalid training-camp admission accepted")
			}
		})
	}
}

/*
================
TestTrainingCampOpenChecksThenBurnsTheFee

The server's camp open check refuses in its order (dead, level, gold, already in a camp) change
nothing; an accepted opening burns the fee, installs the camp and its one
master row, books an academy_fee ledger row, and survives a reopen.
================
*/
func TestTrainingCampOpenChecksThenBurnsTheFee(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	master, other := guildTestCharacter("openmaster"), guildTestCharacter("openother")
	for _, c := range []*domain.Character{master, other} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}
	const fee, floor = 100000, 60
	opening := domain.TrainingCampOpening{MasterCharID: master.ID, MinLevel: floor, Fee: fee}
	set := func(level, gold, hp int64) {
		s.MutateCharacter(master, "test-open-stage", func() {
			master.Level, master.Gold = &level, &gold
			if hp < 0 {
				master.CurrentHP = nil
			} else {
				master.CurrentHP = &hp
			}
		})
	}
	for _, tc := range []struct {
		name            string
		level, gold, hp int64
		want            domain.TrainingCampOpenRefusal
	}{
		// The dead test comes first, ahead of a level and gold that also fail.
		{"dead", 1, 0, 0, domain.TrainingCampOpenDead},
		{"level", floor - 1, fee, -1, domain.TrainingCampOpenLevel},
		{"gold", floor, fee - 1, -1, domain.TrainingCampOpenGold},
	} {
		set(tc.level, tc.gold, tc.hp)
		if _, refusal := s.TrainingCamps().OpenCamp(testDivision, opening); refusal != tc.want {
			t.Fatalf("%s: refusal %d, want %d", tc.name, refusal, tc.want)
		}
		if _, joined := s.TrainingCamps().CampOfCharacter(testDivision, master.ID); joined {
			t.Fatalf("%s: a refused opening made a camp", tc.name)
		}
		if *master.Gold != tc.gold {
			t.Fatalf("%s: a refused opening moved gold", tc.name)
		}
	}

	set(floor, fee+5, -1)
	opened, refusal := s.TrainingCamps().OpenCamp(testDivision, opening)
	if refusal != domain.TrainingCampOpenAccepted || opened.Gold != 5 || *master.Gold != 5 {
		t.Fatalf("open: %+v refusal %d gold %d", opened, refusal, *master.Gold)
	}
	if opened.Camp.ID != master.ID || opened.Camp.MasterCharID != master.ID ||
		opened.Camp.CreatedAtMs != clock.Now().UnixMilli() ||
		!reflect.DeepEqual(opened.Members, []domain.TrainingCampMemberRecord{{
			CharID:     master.ID,
			Kind:       0,
			JoinedAtMs: clock.Now().UnixMilli(),
			JoinLevel:  floor,
		}}) {
		t.Fatalf("opened camp %+v", opened)
	}
	rows := ledgerRows(t, s)
	last := rows[len(rows)-1]
	if last.charID != master.ID || last.reason != "academy_fee" || last.gold != -fee || last.balance != 5 {
		t.Fatalf("ledger row %+v", last)
	}

	set(floor, 10*fee, -1)
	if _, refusal := s.TrainingCamps().OpenCamp(testDivision, opening); refusal != domain.TrainingCampOpenInCamp {
		t.Fatalf("second opening: refusal %d", refusal)
	}
	if *master.Gold != 10*fee {
		t.Fatal("a refused second opening moved gold")
	}

	s.Close()
	reopened := openTest(t, dir, clock)
	camp, members, ok := reopened.TrainingCamps().Camp(testDivision, opened.Camp.ID)
	if !ok || camp != opened.Camp || !reflect.DeepEqual(members, opened.Members) {
		t.Fatalf("reopened camp %+v %+v %v", camp, members, ok)
	}
}

/*
================
TestTrainingCampHonorLifecycleAndPersistence

Verifies DisbandCamp (7d creation wait, SQLite removal, 7d reopen wait),
LeaveCamp (24h join wait), KickMember (24h join wait, campless vs non-master
refusals, case-insensitive lookup, level-40 kick allowed), Graduate and
CompulsoryGraduate (level-41 floor, 24h join wait, mentor honor points, camp
evaluation points, Top-50 tier rankings, and SpendHonorPoints).
================
*/
func TestTrainingCampHonorLifecycleAndPersistence(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)

	master := guildTestCharacter("honormaster")
	student1 := guildTestCharacter("honorstud1")
	student2 := guildTestCharacter("honorstud2")
	student3 := guildTestCharacter("honorstud3")
	outsider := guildTestCharacter("honorout")
	for i, c := range []*domain.Character{master, student1, student2, student3, outsider} {
		acct := "test-account"
		if i >= 4 {
			acct = "test-account-2"
		}
		if err := s.CreateCharacter(testDivision, acct, c); err != nil {
			t.Fatal(err)
		}
	}
	setLevel := func(c *domain.Character, lvl int64) {
		s.MutateCharacter(c, "test-level", func() {
			c.Level = &lvl
		})
	}
	setLevel(master, 60)
	setLevel(student1, 10)
	setLevel(student2, 20)
	setLevel(student3, 30)

	now0 := clock.Now().UnixMilli()
	opened, refusal := s.TrainingCamps().OpenCamp(testDivision, domain.TrainingCampOpening{
		MasterCharID: master.ID,
		MinLevel:     60,
		Fee:          0,
		NowMs:        now0,
	})
	if refusal != domain.TrainingCampOpenAccepted {
		t.Fatalf("open camp refused: %d", refusal)
	}

	// Disband before 7 days (604,800 s, the reference server's disband-wait check) refuses with TrainingCampDisbandWait and remaining seconds.
	disbandEarly := s.TrainingCamps().DisbandCamp(testDivision, master.ID, now0+3600_000)
	if disbandEarly.Refusal != domain.TrainingCampDisbandWait || disbandEarly.RemainingSeconds != 604800-3600 {
		t.Fatalf("early disband = %+v", disbandEarly)
	}

	// Admit three students at levels 10, 20, 30.
	for _, st := range []*domain.Character{student1, student2, student3} {
		if _, ok := s.TrainingCamps().AdmitStudent(testDivision, domain.TrainingCampAdmission{
			MasterCharID:    master.ID,
			StudentCharID:   st.ID,
			ExpectedCampID:  opened.Camp.ID,
			MasterMinLevel:  60,
			StudentMaxLevel: 39,
			StudentLimit:    5,
			NowMs:           now0,
		}); !ok {
			t.Fatalf("admit %s failed", st.Name)
		}
	}

	// Master cannot leave via LeaveCamp; student 3 cannot leave before 24h (the server's leave handler asks its join-cooldown check).
	if res := s.TrainingCamps().LeaveCamp(testDivision, master.ID, now0); res.Refusal != domain.TrainingCampLeaveIsMaster {
		t.Fatalf("master leave = %+v", res)
	}
	if res := s.TrainingCamps().LeaveCamp(testDivision, student3.ID, now0+1000); res.Refusal != domain.TrainingCampLeaveWait24h {
		t.Fatalf("student3 early leave = %+v, want TrainingCampLeaveWait24h", res)
	}
	now24h := now0 + campCompulsoryWaitMs
	leaveRes := s.TrainingCamps().LeaveCamp(testDivision, student3.ID, now24h)
	if leaveRes.Refusal != domain.TrainingCampLeaveAccepted || leaveRes.RemovedMember.CharID != student3.ID {
		t.Fatalf("student3 leave after 24h = %+v", leaveRes)
	}

	// Re-admit student 3 at now24h, then level student 1, student 2, and student 3 to 40.
	if _, ok := s.TrainingCamps().AdmitStudent(testDivision, domain.TrainingCampAdmission{
		MasterCharID:    master.ID,
		StudentCharID:   student3.ID,
		ExpectedCampID:  opened.Camp.ID,
		MasterMinLevel:  60,
		StudentMaxLevel: 39,
		StudentLimit:    5,
		NowMs:           now24h,
	}); !ok {
		t.Fatal("re-admit student3 failed")
	}
	setLevel(student1, 40)
	setLevel(student2, 40)
	setLevel(student3, 40)

	// Kick checks:
	//   - campless caller -> TrainingCampKickNotInCamp (0x680B)
	//   - non-master in camp -> TrainingCampKickNotMaster (0x6816)
	//   - target joined < 24h ago -> TrainingCampKickWait24h (0x6820)
	//   - target joined >= 24h ago -> succeeds even at level 40 (and matches name case-insensitively).
	if kickOut := s.TrainingCamps().KickMember(testDivision, outsider.ID, student3.Name, now24h); kickOut.Refusal != domain.TrainingCampKickNotInCamp {
		t.Fatalf("outsider kick = %+v, want TrainingCampKickNotInCamp", kickOut)
	}
	if kickNotMaster := s.TrainingCamps().KickMember(testDivision, student1.ID, student3.Name, now24h); kickNotMaster.Refusal != domain.TrainingCampKickNotMaster {
		t.Fatalf("student kick = %+v, want TrainingCampKickNotMaster", kickNotMaster)
	}
	if kickEarly := s.TrainingCamps().KickMember(testDivision, master.ID, student3.Name, now24h+1000); kickEarly.Refusal != domain.TrainingCampKickWait24h {
		t.Fatalf("early kick = %+v, want TrainingCampKickWait24h", kickEarly)
	}
	now48h := now24h + campCompulsoryWaitMs
	kick40 := s.TrainingCamps().KickMember(testDivision, master.ID, "HONORSTUD3", now48h)
	if kick40.Refusal != domain.TrainingCampKickAccepted || kick40.RemovedMember.CharID != student3.ID {
		t.Fatalf("kick level-40 student3 after 24h = %+v", kick40)
	}

	// Student 1 (joinLevel=10) graduates voluntarily with satisfaction 4 (Very Satisfied):
	// Master honor = 40-10 = 30, camp evaluation = +5 (the reference server's satisfaction ladder -3, -1, 1, 3, 5 at index 4), GraduateCount = 1, Rank = 1 (ranking 1 <= 5).
	grad1 := s.TrainingCamps().Graduate(testDivision, student1.ID, 4, now0+10_000)
	if grad1.Refusal != domain.TrainingCampGraduateAccepted || !grad1.AwardedExp ||
		grad1.Camp.GraduateCount != 1 || grad1.Camp.EvaluationPoints != 5 || grad1.Camp.EvaluationRank != 1 ||
		len(grad1.HonorDeltas) != 1 || grad1.HonorDeltas[0].HonorPoints != 30 {
		t.Fatalf("grad1 = %+v", grad1)
	}

	// Compulsory graduation of student 2:
	//   - at level 40 (< 41: the server compares the level with 0x28 and refuses with 0x6822) refuses with TrainingCampGraduateCompulsoryLevelLow;
	//   - at level 41 succeeds with satisfaction 2 (+1 evaluation point, as the server's compulsory graduation grants, +20 honor points).
	if compLvl40 := s.TrainingCamps().CompulsoryGraduate(testDivision, master.ID, student2.Name, now24h); compLvl40.Refusal != domain.TrainingCampGraduateCompulsoryLevelLow {
		t.Fatalf("level-40 compulsory grad = %+v, want TrainingCampGraduateCompulsoryLevelLow", compLvl40)
	}
	setLevel(student2, 41)
	grad2 := s.TrainingCamps().CompulsoryGraduate(testDivision, master.ID, "HONORSTUD2", now24h)
	if grad2.Refusal != domain.TrainingCampGraduateAccepted || grad2.AwardedExp ||
		grad2.Camp.GraduateCount != 2 || grad2.Camp.EvaluationPoints != 6 ||
		len(grad2.HonorDeltas) != 1 || grad2.HonorDeltas[0].HonorPoints != 50 {
		t.Fatalf("grad2 = %+v", grad2)
	}

	// Spend 18 honor points -> 32 remaining.
	remHonor, _, ok := s.TrainingCamps().SpendHonorPoints(testDivision, master.ID, 18)
	if !ok || remHonor != 32 || s.TrainingCamps().HonorPoints(testDivision, master.ID) != 32 {
		t.Fatalf("spend honor = %d ok=%v", remHonor, ok)
	}

	rankings := s.TrainingCamps().HonorRankings(testDivision)
	if len(rankings) != 1 || rankings[0].Ranking != 1 || rankings[0].Rank != 1 ||
		rankings[0].MasterName != master.Name || rankings[0].GraduateCount != 2 {
		t.Fatalf("rankings = %+v", rankings)
	}

	// Disband after 7 days removes the camp from memory and SQLite, and arms the 7-day reopen cooldown.
	now7d := now0 + campDisbandCooldownSec*1000
	disband := s.TrainingCamps().DisbandCamp(testDivision, master.ID, now7d)
	if disband.Refusal != domain.TrainingCampDisbandAccepted {
		t.Fatalf("disband = %+v", disband)
	}
	if _, ok := s.TrainingCamps().CampOfCharacter(testDivision, master.ID); ok {
		t.Fatal("master still in camp after disband")
	}
	reopenWait, refWait := s.TrainingCamps().OpenCamp(testDivision, domain.TrainingCampOpening{
		MasterCharID: master.ID,
		MinLevel:     60,
		Fee:          0,
		NowMs:        now7d + 1000_000,
	})
	if refWait != domain.TrainingCampOpenReopenWait || reopenWait.RemainingSeconds != 604800-1000 {
		t.Fatalf("reopen wait = %+v ref=%d", reopenWait, refWait)
	}

	s.Close()
	reopened := openTest(t, dir, clock)
	if _, _, ok := reopened.TrainingCamps().Camp(testDivision, opened.Camp.ID); ok {
		t.Fatal("disbanded camp survived reopen")
	}
}

/*
================
TestTrainingCampGraduationHonorFormulasAndTierBoundaries

Verifies the reference server's graduation handler, its camp graduation-points
update and its graduation honor award, and the
_TRAINING_CAMP_MEMBER_GRADUATE / _TRAINING_CAMP_UPDATEHONORRANK rules:
  - joinLevel <= 30 grants Master (40 - joinLevel) and Sub-Mentor (40 - joinLevel)/10
    even when satisfaction == 0 (-3 evaluation delta).
  - joinLevel > 30 increments GraduateCount by 1 and awards 0 Honor and 0 Evaluation.
  - Honor Rank tier cutoffs: rankings 1..5 -> 1, 6..15 -> 2, 16..30 -> 3, 31..50 -> 4.

================
*/
func TestTrainingCampGraduationHonorFormulasAndTierBoundaries(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)

	master := guildTestCharacter("formmaster")
	subMentor := guildTestCharacter("formsub")
	app15 := guildTestCharacter("formapp15")
	app31 := guildTestCharacter("formapp31")
	for _, c := range []*domain.Character{master, subMentor, app15, app31} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}
	setLevel := func(c *domain.Character, lvl int64) {
		s.MutateCharacter(c, "test-level", func() {
			c.Level = &lvl
		})
	}
	setLevel(master, 60)
	setLevel(subMentor, 45)
	setLevel(app15, 15)
	setLevel(app31, 31)

	now0 := clock.Now().UnixMilli()
	opened, refusal := s.TrainingCamps().OpenCamp(testDivision, domain.TrainingCampOpening{
		MasterCharID: master.ID,
		MinLevel:     60,
		Fee:          0,
		NowMs:        now0,
	})
	if refusal != domain.TrainingCampOpenAccepted {
		t.Fatalf("open camp refused: %d", refusal)
	}

	// Admit sub-mentor (level 45 -> Kind 1) and two apprentices (levels 15 and 31 -> Kind 2).
	for _, st := range []*domain.Character{subMentor, app15, app31} {
		if _, ok := s.TrainingCamps().AdmitStudent(testDivision, domain.TrainingCampAdmission{
			MasterCharID:    master.ID,
			StudentCharID:   st.ID,
			ExpectedCampID:  opened.Camp.ID,
			MasterMinLevel:  60,
			StudentMaxLevel: 59,
			StudentLimit:    5,
			NowMs:           now0,
		}); !ok {
			t.Fatalf("admit %s failed", st.Name)
		}
	}

	// Stage subMentor as Kind 1 (Sub-Mentor: the reference server assigns Kind 1 when level > 0x27).
	s.campMembers[testDivision][opened.Camp.ID][1].Kind = 1

	setLevel(app15, 40)
	setLevel(app31, 40)

	// Apprentice joined at level 15 graduates with satisfaction 0 (Very Dissatisfied -> -3 eval):
	// Master gets 40-15 = 25 honor, Sub-Mentor gets 25/10 = 2 honor, EvaluationPoints = -3.
	grad15 := s.TrainingCamps().Graduate(testDivision, app15.ID, 0, now0+1000)
	if grad15.Refusal != domain.TrainingCampGraduateAccepted || !grad15.AwardedExp ||
		grad15.Camp.GraduateCount != 1 || grad15.Camp.EvaluationPoints != -3 ||
		len(grad15.HonorDeltas) != 2 ||
		grad15.HonorDeltas[0].CharID != master.ID || grad15.HonorDeltas[0].HonorPoints != 25 ||
		grad15.HonorDeltas[1].CharID != subMentor.ID || grad15.HonorDeltas[1].HonorPoints != 2 {
		t.Fatalf("grad15 = %+v", grad15)
	}

	// Apprentice joined at level 31 (> 30) graduates with satisfaction 4:
	// GraduateCount increments to 2, but 0 honor and 0 evaluation points are awarded.
	grad31 := s.TrainingCamps().Graduate(testDivision, app31.ID, 4, now0+2000)
	if grad31.Refusal != domain.TrainingCampGraduateAccepted || !grad31.AwardedExp ||
		grad31.Camp.GraduateCount != 2 || grad31.Camp.EvaluationPoints != -3 ||
		len(grad31.HonorDeltas) != 0 {
		t.Fatalf("grad31 = %+v", grad31)
	}

	// Verify honorTierRankForIndex boundaries (0-based index -> 1-based ranking):
	for idx, want := range map[int]uint8{
		0: 1, 4: 1,
		5: 2, 14: 2,
		15: 3, 29: 3,
		30: 4, 49: 4,
	} {
		if got := honorTierRankForIndex(idx); got != want {
			t.Fatalf("honorTierRankForIndex(%d) = %d, want %d", idx, got, want)
		}
	}
}
