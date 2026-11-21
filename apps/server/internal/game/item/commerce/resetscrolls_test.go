/*
===========================================================================

resetscrolls_test.go - the reset scrolls' table, switches and listing

Port-only, not native. Each scroll lists only while its own switch is
on; the all-levels stat scroll never turns the tier scrolls' level
windows on.

===========================================================================
*/
package commerce

import (
	"reflect"
	"testing"
)

/*
================
TestResetScrollMallGoodsFollowFlags
================
*/
func TestResetScrollMallGoodsFollowFlags(t *testing.T) {
	if goods := ResetScrollMallGoods(nil); len(goods) != 0 {
		t.Fatalf("every switch off lists %+v, want nothing", goods)
	}
	got := ResetScrollMallGoods(map[ResetScrollKind]bool{SkillReset: true})
	want := []MallExtraGood{{Tab: "MALL_CONSUME_SCROLL", Package: "PACKAGE_ITEM_MALL_SKILL_POINT_RECALL_A",
		Item: "ITEM_MALL_SKILL_POINT_RECALL_A", Silk: 690}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skill switch on lists %+v, want %+v", got, want)
	}
	got = ResetScrollMallGoods(map[ResetScrollKind]bool{StatReset: true, SkillReset: true})
	if len(got) != 2 || got[0].Item != "ITEM_MALL_STATS_POINT_RECALL_ALL" || got[0].Silk != 690 ||
		got[0].Package != "PACKAGE_ITEM_MALL_STATS_POINT_RECALL_ALL" {
		t.Fatalf("both on list %+v, want the stat scroll first at 690 silk", got)
	}
}

/*
================
TestResetScrollFromEnv
================
*/
func TestResetScrollFromEnv(t *testing.T) {
	scroll, ok := ResetScrollByCodename("ITEM_MALL_SKILL_POINT_RECALL_A")
	if !ok || scroll.Kind != SkillReset || scroll.Env != EnvSkillResetScroll {
		t.Fatalf("skill scroll = %+v, %t", scroll, ok)
	}
	if _, ok := ResetScrollByCodename("ITEM_MALL_STATS_POINT_RECALL"); ok {
		t.Fatal("a tier scroll is no reset scroll")
	}
	t.Setenv(EnvSkillResetScroll, "on")
	if !ResetScrollEnabledFromEnv(scroll) {
		t.Fatal("on reads as off")
	}
	t.Setenv(EnvSkillResetScroll, "")
	if ResetScrollEnabledFromEnv(scroll) {
		t.Fatal("unset reads as on")
	}
}

/*
================
TestStatRecallListedIgnoresTheAllLevelsScroll
================
*/
func TestStatRecallListedIgnoresTheAllLevelsScroll(t *testing.T) {
	all := ResetScrollMallGoods(map[ResetScrollKind]bool{StatReset: true})
	if StatRecallListed(all) {
		t.Fatal("the all-levels scroll alone turned the tier windows on")
	}
	tier := MallExtraGood{Tab: "MALL_CONSUME_SCROLL", Package: "PACKAGE_ITEM_MALL_STATS_POINT_RECALL",
		Item: "ITEM_MALL_STATS_POINT_RECALL", Silk: 30}
	if !StatRecallListed(append(all, tier)) {
		t.Fatal("a tier scroll no longer turns the windows on")
	}
}
