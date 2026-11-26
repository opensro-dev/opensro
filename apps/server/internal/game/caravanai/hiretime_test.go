package caravanai

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestHunterPoolKeepsTwoFree

Hunters hired in a town always leave HirePoolReserve free for other
players: the hire that would take one of the last two is refused, and the
hire window shows them as kept free.
================
*/
func TestHunterPoolKeepsTwoFree(t *testing.T) {
	players := map[string]*hirePlayer{}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	rt.mu.Lock()
	free := rt.freeOfRoleLocked("division", RoleHunter, TownJangan)
	rt.mu.Unlock()
	if free <= HirePoolReserve {
		t.Skipf("only %d free Hunters in Jangan", free)
	}
	hired := 0
	for i, u := range rt.EnsureDivisionRoster("division") {
		if u.Role != RoleHunter {
			continue
		}
		rt.mu.Lock()
		home := rt.homeLocked(u)
		rt.mu.Unlock()
		if home != TownJangan {
			continue
		}
		player := fmt.Sprintf("P%d", i)
		players[player] = &hirePlayer{at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}
		reply := rt.HireAgent("division", player, domain.JobTrader, u.Name, 1000)
		if strings.Contains(reply, "joins you") {
			hired++
			continue
		}
		if !strings.Contains(reply, "stay for other players") {
			t.Fatalf("hire refused for another reason: %s", reply)
		}
		break
	}
	if hired != free-HirePoolReserve {
		t.Fatalf("hired %d of %d free Hunters, want all but %d", hired, free, HirePoolReserve)
	}
	players["Late"] = &hirePlayer{at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}
	kept := 0
	for _, row := range rt.HireRoster("division", "Late", domain.JobTrader, TownJangan, 1000) {
		if row.Role == RoleHunter && row.Town == TownJangan && row.Status == "Kept free for others" {
			kept++
		}
	}
	if kept != HirePoolReserve {
		t.Fatalf("the window shows %d Hunters kept free, want %d", kept, HirePoolReserve)
	}
}

/*
================
TestHunterHireHourWarnsEndsAndExtends

A Hunter's hire warns its hirer HireWarnMs before its hour runs out and
ends when it does; renewing at a shop adds an hour, never more than two
ahead.
================
*/
func TestHunterHireHourWarnsEndsAndExtends(t *testing.T) {
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	var notices []string
	var ended []HireEnd
	rt.hooks.OnHireNotice = func(_, _, text string) { notices = append(notices, text) }
	rt.hooks.OnHireEnded = func(_, _, _ string, end HireEnd, _ int64) { ended = append(ended, end) }
	step := func(now int64) *AgentUnit {
		_, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Hunter_Kwan", nil, now)
		return unit
	}
	rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Hunter_Kwan", 1000)
	step(1000 + HireDurationMs - HireWarnMs - 1)
	if len(notices) != 0 {
		t.Fatalf("warned early: %v", notices)
	}
	step(1000 + HireDurationMs - HireWarnMs)
	if len(notices) != 1 || !strings.Contains(notices[0], "ends in 5 minutes") {
		t.Fatalf("no warning before the hour ran out: %v", notices)
	}
	// Renewed: another hour; a second renewal now would go past two hours.
	if reply := rt.ExtendHire("division", "Boss", "[AI]Hunter_Kwan", 1000+HireDurationMs-HireWarnMs); !strings.Contains(reply, "extended") {
		t.Fatalf("renewal refused: %s", reply)
	}
	if reply := rt.ExtendHire("division", "Boss", "[AI]Hunter_Kwan", 1000+HireDurationMs-HireWarnMs); !strings.Contains(reply, "already hired") {
		t.Fatalf("renewed past two hours: %s", reply)
	}
	if unit := step(1000 + HireDurationMs); unit.HiredBy != "Boss" {
		t.Fatal("a renewed hire ended at its first hour")
	}
	if unit := step(1000 + 2*HireDurationMs); unit.HiredBy != "" || len(ended) != 1 || ended[0] != HireTimedOut {
		t.Fatalf("the renewed hour did not end the hire: %q %v", unit.HiredBy, ended)
	}
}

/*
================
TestHunterOnlyHireEndsAfterTwentyMinutesOut

Hired with no Trader, a Hunter's hire ends when its hirer is back in a
town after at least HireOutRunMs out; a shorter trip does not count.
================
*/
func TestHunterOnlyHireEndsAfterTwentyMinutesOut(t *testing.T) {
	field := Waypoint{RegionID: 25239, X: 900, Z: 900}
	players := map[string]*hirePlayer{"Boss": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	step := func(now int64) *AgentUnit {
		_, unit, _ := rt.StepAgent(context.Background(), "division", "[AI]Hunter_Kwan", nil, now)
		return unit
	}
	rt.HireAgent("division", "Boss", domain.JobTrader, "[AI]Hunter_Kwan", 1000)
	players["Boss"].at = field
	step(2000)
	players["Boss"].at = TownWaypoint(TownJangan)
	if unit := step(2000 + HireOutRunMs - 1000); unit.HiredBy != "Boss" {
		t.Fatal("a short trip out ended the hire")
	}
	players["Boss"].at = field
	step(3_000_000)
	players["Boss"].at = TownWaypoint(TownDonwhang)
	if unit := step(3_000_000 + HireOutRunMs); unit.HiredBy != "" {
		t.Fatal("the hire outlived a run of twenty minutes out")
	}
}
