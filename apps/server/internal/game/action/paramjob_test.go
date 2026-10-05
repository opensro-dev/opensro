/*
===========================================================================

paramjob_test.go - composite scrolls and their param jobs

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
internalScroll

An internal param item shaped like ITEM_ETC_INTERNAL_150EXP_SCROLL.
================
*/
func internalScroll(id uint32, codename string, fourCC, value float64) *enterworld.ItemRef {
	return &enterworld.ItemRef{RefObjID: id, Codename: codename, TypeIDs: [4]int64{3, 3, 3, 10},
		NativeFields: enterworld.NewNativeFields(map[string]float64{
			"itemParam1_29c": 3600, "itemParam2_2a0": fourCC, "itemParam3_2a4": value})}
}

func TestCompositeScrollListAndCooltimeParse(t *testing.T) {
	ref := &enterworld.ItemRef{Codename: "ITEM_ETC_150EXP_BASIC", TypeIDs: [4]int64{3, 3, 13, 14},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"itemParam2_2a0": 1, "itemParam6_2b0": 3600000})}
	ref.ParamDescriptions[1] = "[UIU1:ITEM_ETC_INTERNAL_150EXP_SCROLL],[UIU1:ITEM_ETC_INTERNAL_150SP_SCROLL]"
	ref.ParamDescriptions[5] = "COOLTIME:0x000000CA"
	entries, ok := compositeEntries(ref)
	if !ok || len(entries) != 2 || entries[1].tag != "UIU1" || len(entries[1].args) != 1 ||
		entries[1].args[0] != "ITEM_ETC_INTERNAL_150SP_SCROLL" {
		t.Fatalf("entries = %v/%v", entries, ok)
	}
	group, ms, ok := itemCooltime(ref)
	if !ok || group != 0xca || ms != 3600000 {
		t.Fatalf("cooltime = %x/%d/%v", group, ms, ok)
	}
	ref.ParamDescriptions[1] = "[UIU1:A],broken"
	if _, ok := compositeEntries(ref); ok {
		t.Fatal("malformed list accepted")
	}
}

func TestParamJobFromInternalItemMapsTheFourCC(t *testing.T) {
	job, ok := paramJobFromItem(internalScroll(7, "ITEM_ETC_INTERNAL_150EXP_SCROLL", 1885696629, 150), 1000)
	if !ok || job.Param != paramExpRate || job.Value != 150 || job.EndUnixMs != 1000+3600*1000 {
		t.Fatalf("exp job = %+v/%v", job, ok)
	}
	job, ok = paramJobFromItem(internalScroll(8, "ITEM_ETC_INTERNAL_100SP_SCROLL", 1886614133, 100), 0)
	if !ok || job.Param != paramSkillExpRate {
		t.Fatalf("sp job = %+v/%v", job, ok)
	}
	if _, ok := paramJobFromItem(internalScroll(9, "X", 0x41414141, 10), 0); ok {
		t.Fatal("unknown FourCC accepted")
	}
}

func TestParamJobBonusAddsLivePercentagesOnly(t *testing.T) {
	c := &enterworld.Character{ParamJobs: []domain.ParamJob{
		{ItemRefObjID: 1, Param: paramExpRate, Value: 150, EndUnixMs: 5000},
		{ItemRefObjID: 2, Param: paramSkillExpRate, Value: 100, EndUnixMs: 5000},
		{ItemRefObjID: 3, Param: paramExpRate, Value: 100, EndUnixMs: 10},
	}}
	exp, sp := paramJobRewardBonus(c, 1000, 40, 100)
	if exp != 2500 || sp != 80 {
		t.Fatalf("bonus = %d/%d, want 2500/80", exp, sp)
	}
	if exp, sp := paramJobRewardBonus(c, 1000, 40, 6000); exp != 1000 || sp != 40 {
		t.Fatalf("expired jobs still pay: %d/%d", exp, sp)
	}
}

func TestParamJobUpsertRestartsTheSameItem(t *testing.T) {
	jobs, _ := upsertParamJob(nil, domain.ParamJob{ItemRefObjID: 1, EndUnixMs: 10})
	jobs, _ = upsertParamJob(jobs, domain.ParamJob{ItemRefObjID: 1, EndUnixMs: 99})
	if len(jobs) != 1 || jobs[0].EndUnixMs != 99 {
		t.Fatalf("jobs = %+v", jobs)
	}
}
