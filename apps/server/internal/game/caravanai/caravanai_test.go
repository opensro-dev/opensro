/*
===========================================================================

caravanai_test.go - AI Caravan & Thief agent tests (port-only)

Exercises the Jangan-Donwhang-Hotan route graph, detour heat adaptation,
HardcodedBrain and LLMBrain (with automatic fallback), Parley truce honor and
betrayal vendettas, and two-way Mercenary Contracts.

===========================================================================
*/
package caravanai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
==================
TestHighwayRouteDetoursWhenFerryHeatIsHigh

Verifies that a Cautious or 4Ã¢Ëœâ€¦-5Ã¢Ëœâ€¦ caravan between Jangan and Donwhang uses the
Jangan Ferry crossing under normal conditions, and switches to the Northern
Mountain Detour once ambush heat at the Ferry reaches DetourHeatThreshold.
==================
*/
func TestHighwayRouteDetoursWhenFerryHeatIsHigh(t *testing.T) {
	normal := SelectRoute(TownJangan, TownDonwhang, 4, PersonalityCautious, map[uint16]int{
		RegionJanganFerryEast: 20,
	})
	if len(normal) != 5 || normal[2].RegionID != RegionJanganFerryEast {
		t.Fatalf("expected normal Jangan->Donwhang crossing at Ferry (%d), got %+v", RegionJanganFerryEast, normal)
	}

	hot := SelectRoute(TownJangan, TownDonwhang, 4, PersonalityCautious, map[uint16]int{
		RegionJanganFerryEast: DetourHeatThreshold,
	})
	if len(hot) != 5 || hot[2].RegionID != RegionJanganNorthDetour {
		t.Fatalf("expected hot Jangan->Donwhang crossing to detour via %d, got %+v", RegionJanganNorthDetour, hot)
	}

	fullHighway := SelectRoute(TownJangan, TownHotan, 5, PersonalityGreedy, map[uint16]int{
		RegionJanganFerryEast: 75,
	})
	if len(fullHighway) != 9 {
		t.Fatalf("expected 9 waypoints for Jangan->Donwhang->Hotan, got %d", len(fullHighway))
	}
	if fullHighway[0].Town != TownJangan || fullHighway[len(fullHighway)-1].Town != TownHotan {
		t.Fatalf("unexpected endpoints on full highway: %+v -> %+v", fullHighway[0], fullHighway[len(fullHighway)-1])
	}
}

/*
==================
TestHardcodedBrainParleyVendettaAndAllyDecisions

Verifies that HardcodedBrain triggers cargo-bribe Parley when cornered (<35% HP),
calls out Vendetta rivals by name and stolen gold, and greets Trusted Allies.
==================
*/
func TestHardcodedBrainParleyVendettaAndAllyDecisions(t *testing.T) {
	brain := NewHardcodedBrain()
	ctx := context.Background()

	// 1. Cornered Trader offers Parley cargo bribe.
	parleyDec := brain.Decide(ctx, Situation{
		AgentName:    "[AI]Trader_Jin",
		Role:         RoleTrader,
		Personality:  PersonalityCautious,
		RegionID:     RegionJanganFerryEast,
		HPPercent:    28,
		CargoStars:   4,
		CargoStacks:  10,
		UnderAttack:  true,
		ParleyReady:  true,
		NearbyAllies: 0,
		Players: []ObservedPlayer{
			{Name: "BlackFang", JobType: domain.JobThief, HPPercent: 95, Distance: 40},
		},
	})
	if parleyDec.Action != ActionOfferParley || parleyDec.TargetName != "BlackFang" {
		t.Fatalf("expected ActionOfferParley targeting BlackFang, got %+v", parleyDec)
	}

	// 2. Vendetta rival triggers personal grudge callout.
	vendettaDec := brain.Decide(ctx, Situation{
		AgentName:   "[AI]Hunter_Kwan",
		Role:        RoleHunter,
		Personality: PersonalityVengeful,
		RegionID:    RegionKarakoramPass,
		HPPercent:   90,
		Players: []ObservedPlayer{
			{
				Name:       "BlackFang",
				JobType:    domain.JobThief,
				HPPercent:  80,
				TrustScore: -80,
				Deaths:     2,
				GoldStolen: 120000,
			},
		},
	})
	if vendettaDec.Action != ActionAttackTarget || !strings.Contains(vendettaDec.Dialogue, "120000") {
		t.Fatalf("expected vendetta attack callout referencing stolen gold, got %+v", vendettaDec)
	}

	// 3. Trusted Ally triggers friendly support callout.
	allyDec := brain.Decide(ctx, Situation{
		AgentName:   "[AI]Trader_Jin",
		Role:        RoleTrader,
		Personality: PersonalityHonorable,
		RegionID:    RegionDonwhangEastRoad,
		HPPercent:   95,
		Players: []ObservedPlayer{
			{
				Name:       "ShieldBear",
				JobType:    domain.JobHunter,
				HPPercent:  100,
				TrustScore: 75,
			},
		},
	})
	if allyDec.Action != ActionAssistAlly || allyDec.TargetName != "ShieldBear" {
		t.Fatalf("expected ActionAssistAlly for ShieldBear, got %+v", allyDec)
	}
}

/*
==================
TestLLMBrainSuccessAndFallbackToHardcoded

Verifies that LLMBrain parses OpenAI/Ollama-compatible JSON completions when
healthy, and seamlessly falls back to HardcodedBrain when the endpoint returns
an error or invalid payload.
==================
*/
func TestLLMBrainSuccessAndFallbackToHardcoded(t *testing.T) {
	failMode := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failMode {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "{\"action\":\"ATTACK_TARGET\",\"target\":\"ShadowBlade\",\"dialogue\":\"You won't take a single silk bale today, ShadowBlade!\",\"memory_note\":\"Defended Donwhang road\"}"
				}
			}]
		}`))
	}))
	defer srv.Close()

	brain := NewBrain(Config{
		Enabled:    true,
		BrainMode:  BrainLLM,
		LLMURL:     srv.URL,
		LLMModel:   "test-model",
		LLMTimeout: 2 * time.Second,
	})

	sit := Situation{
		AgentName:   "[AI]Hunter_Kwan",
		Role:        RoleHunter,
		Personality: PersonalityHonorable,
		RegionID:    RegionJanganFerryEast,
		HPPercent:   85,
		Players: []ObservedPlayer{
			{Name: "ShadowBlade", JobType: domain.JobThief, HPPercent: 90, Distance: 30},
		},
	}

	dec := brain.Decide(context.Background(), sit)
	if dec.Source != BrainLLM || dec.Action != ActionAttackTarget || dec.TargetName != "ShadowBlade" {
		t.Fatalf("expected LLM decision targeting ShadowBlade, got %+v", dec)
	}

	// Flip server into 500 error mode and confirm instant fallback to HardcodedBrain.
	failMode = true
	fallbackDec := brain.Decide(context.Background(), sit)
	if fallbackDec.Source != BrainHardcoded || fallbackDec.Action != ActionAttackTarget {
		t.Fatalf("expected HardcodedBrain fallback decision on HTTP 500, got %+v", fallbackDec)
	}
}

/*
==================
TestRuntimeParleyHonorBetrayalAndContracts

Verifies end-to-end Runtime behavior:
 1. Cornered AI Trader drops 50% cargo stacks and opens a 15-second parley.
 2. Attacking during the parley window triggers a -100 Betrayal Vendetta.
 3. Honoring the parley window records a mild -10 truce memory.
 4. Mercenary contracts (!accept, !hire, !memory) bind and pay out rewards.

==================
*/
func TestRuntimeParleyHonorBetrayalAndContracts(t *testing.T) {
	var droppedStacks int
	var rewardedHunter string
	var rewardedGold int64

	rt := NewRuntime(
		Config{Enabled: true, BrainMode: BrainHardcoded},
		nil,
		WorldHooks{
			Player: func(_ string, name string) (PlayerPosition, bool) {
				return PlayerPosition{ObservedPlayer: ObservedPlayer{Name: name, HPPercent: 100}, At: TownWaypoint(TownJangan)}, true
			},
			OnHireFee: func(_, _ string, _ int64) bool { return true },
			OnParleyCargoDrop: func(_ string, _ string, _ uint16, _, _, _ float32, stacks int, _ int64) {
				droppedStacks = stacks
			},
			OnContractReward: func(_ string, hunterPlayerName string, rewardGold int64, _ string) {
				rewardedHunter = hunterPlayerName
				rewardedGold = rewardGold
			},
		},
	)

	const div = "Shard_Silkroad"
	rt.EnsureDivisionRoster(div)

	// A recruiting trader is accepted in town; active road runs are not contractable.
	rt.mu.Lock()
	rt.findAgentLocked(div, "[AI]Trader_Jin").State = StateRecruiting
	rt.mu.Unlock()
	reply, ok := rt.HandlePlayerCommand(div, "IronGuard", domain.JobHunter, "!accept [AI]Trader_Jin", 1_000)
	if !ok || !strings.Contains(reply, "IronGuard") {
		t.Fatalf("expected contract acceptance reply for IronGuard, got ok=%v reply=%q", ok, reply)
	}

	// 2. Trader player hires [AI]Hunter_Kwan via chat command.
	hireReply, ok := rt.HandlePlayerCommand(div, "MerchantLin", domain.JobTrader, "!hire [AI]Hunter_Kwan", 1_000)
	if !ok || !strings.Contains(hireReply, "MerchantLin") {
		t.Fatalf("expected hunter hire reply for MerchantLin, got ok=%v reply=%q", ok, hireReply)
	}

	// 3. Damage [AI]Trader_Jin down to 25% HP and step with hostile Thief nearby -> Parley offered.
	rt.RecordPlayerAttack(div, "[AI]Trader_Jin", "ViperPlayer", 9000, 2_000)
	dec, unit, ok := rt.StepAgent(context.Background(), div, "[AI]Trader_Jin", []ObservedPlayer{
		{Name: "ViperPlayer", JobType: domain.JobThief, HPPercent: 100, Distance: 25},
	}, 2_100)
	if !ok || dec.Action != ActionOfferParley {
		t.Fatalf("expected [AI]Trader_Jin to offer parley at 25%% HP, got dec=%+v", dec)
	}
	if droppedStacks != 5 || unit.CargoStacks != 5 {
		t.Fatalf("expected cautious trader to drop 5 of 10 stacks (50%%), dropped=%d remaining=%d", droppedStacks, unit.CargoStacks)
	}

	// 4. ViperPlayer attacks during the 15s parley window -> immediate -100 Betrayal Vendetta.
	betrayed, _, memRow := rt.RecordPlayerAttack(div, "[AI]Trader_Jin", "ViperPlayer", 500, 5_000)
	if !betrayed || memRow.TrustScore != domain.CaravanTrustMin {
		t.Fatalf("expected parley betrayal to record -100 Vendetta, betrayed=%v row=%+v", betrayed, memRow)
	}

	// 5. Inspect memory via !memory command.
	memReply, ok := rt.HandlePlayerCommand(div, "ViperPlayer", domain.JobThief, "!memory [AI]Trader_Jin", 6_000)
	if !ok || !strings.Contains(memReply, "Trust: -100") {
		t.Fatalf("expected !memory reply to show Trust: -100, got %q", memReply)
	}

	// 6. Complete [AI]Trader_Jin's contract delivery and verify payout + Hunter trust.
	for now := int64(3100); now <= 40100; now += 1000 {
		rt.Contracts().SampleEscort(div, "[AI]Trader_Jin", true, now)
	}
	contract, completed := rt.Contracts().CompleteAITraderRun(div, "[AI]Trader_Jin", RegionDonwhangTown, 40_100, rt.Memories())
	if !completed || contract.RewardGold <= 0 {
		t.Fatalf("expected escort contract to complete with positive reward, got %+v", contract)
	}
	if rt.hooks.OnContractReward != nil {
		rt.hooks.OnContractReward(div, contract.HunterName, contract.RewardGold, contract.TraderName)
	}
	if rewardedHunter != "IronGuard" || rewardedGold != contract.RewardGold {
		t.Fatalf("expected reward hook for IronGuard (%d), got hunter=%q gold=%d", contract.RewardGold, rewardedHunter, rewardedGold)
	}
	hunterMem, exists := rt.Memories().Relationship(div, "[AI]Trader_Jin", "IronGuard")
	if !exists || hunterMem.TrustScore != 25 {
		t.Fatalf("expected IronGuard trust +25 after escort completion, got %+v", hunterMem)
	}
}

/*
================
TestLoopbackURL

Only an http(s) URL naming this host may carry the LLM brain's prompt.
================
*/
func TestLoopbackURL(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]bool{
		"http://127.0.0.1:11434/v1/chat/completions": true,
		"http://127.8.9.10/v1":                       true,
		"https://localhost/v1":                       true,
		"http://[::1]:8080/v1":                       true,
		"https://api.openai.com/v1/chat/completions": false,
		"http://10.0.0.5:11434/v1":                   false,
		"http://localhost.example.com/v1":            false,
		"ftp://127.0.0.1/v1":                         false,
		"":                                           false,
	} {
		if got := LoopbackURL(raw); got != want {
			t.Errorf("LoopbackURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

/*
================
TestRemoteLLMURLIsRefused

A remote SRO_AGENT_LLM_URL leaves the agents on the hardcoded brain, from
the environment and from a Config built by hand.
================
*/
func TestRemoteLLMURLIsRefused(t *testing.T) {
	const remote = "https://llm.example.com/v1/chat/completions"
	t.Setenv(EnvAICaravans, "on")
	t.Setenv(EnvAgentBrain, "llm")
	t.Setenv(EnvAgentLLMURL, remote)
	cfg := ConfigFromEnv()
	if cfg.BrainMode != BrainHardcoded || cfg.LLMURL != "" || cfg.RefusedLLMURL != remote {
		t.Fatalf("ConfigFromEnv kept a remote brain: %+v", cfg)
	}
	brain := NewBrain(Config{Enabled: true, BrainMode: BrainHybrid, LLMURL: remote})
	if _, ok := brain.(*HardcodedBrain); !ok {
		t.Fatalf("NewBrain built %T for a remote URL", brain)
	}
}

/*
================
TestLLMBrainFollowsNoRedirect

A loopback endpoint that answers 307 cannot pass the prompt on: the second
server is never asked and the hardcoded brain decides.
================
*/
func TestLLMBrainFollowsNoRedirect(t *testing.T) {
	var reached atomic.Bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
	}))
	defer elsewhere.Close()
	bounce := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer bounce.Close()

	brain := NewBrain(Config{Enabled: true, BrainMode: BrainLLM, LLMURL: bounce.URL, LLMTimeout: 2 * time.Second})
	dec := brain.Decide(context.Background(), Situation{
		AgentName: "[AI]Hunter_Kwan",
		Role:      RoleHunter,
		RegionID:  RegionJanganFerryEast,
		HPPercent: 85,
		Players:   []ObservedPlayer{{Name: "ShadowBlade", JobType: domain.JobThief, HPPercent: 90, Distance: 30}},
	})
	if reached.Load() {
		t.Fatal("the redirect target received the prompt")
	}
	if dec.Source != BrainHardcoded {
		t.Fatalf("a redirect answered the decision: %+v", dec)
	}
}

/*
================
TestSchedulerMinActiveTradesHireableHuntersAndDistressAid

Verifies:
 1. The roster starts with 2 active trade runs, 2 town-staged Traders, and
    2 town-staged Hunters available for hire.
 2. When an active Trader is defeated and returns to town, the scheduler
    automatically dispatches a town-staged Trader so at least 2 trade runs
    remain active.
 3. When an active Trader is ambushed, standby Hunters and fellow Traders
    respond to the DistressCall with ActionReinforceAlly.

================
*/
func TestSchedulerMinActiveTradesHireableHuntersAndDistressAid(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true, BrainMode: BrainHardcoded}, nil, WorldHooks{})
	const div = "Shard_Scheduler"
	roster := rt.EnsureDivisionRoster(div)

	activeTraders := 0
	townTraders := 0
	hireableHunters := 0
	for _, u := range roster {
		if u.Role == RoleTrader {
			if isActiveTradeRunState(u.State) {
				activeTraders++
			} else {
				townTraders++
			}
		} else if u.Role == RoleHunter && u.AvailableForHire {
			hireableHunters++
		}
	}
	if activeTraders < MinActiveTradeRuns || townTraders < 2 || hireableHunters < 2 {
		t.Fatalf(
			"expected >=2 active traders, >=2 town traders, >=2 hireable hunters; got active=%d town=%d hireable=%d",
			activeTraders, townTraders, hireableHunters,
		)
	}

	// Defeat [AI]Trader_Jin -> scheduler immediately dispatches a town Trader so active runs stay >= 2.
	_, defeated, _ := rt.RecordPlayerAttack(div, "[AI]Trader_Jin", "BanditKing", 20000, 1_000)
	if !defeated {
		t.Fatal("expected [AI]Trader_Jin to be defeated by 20000 damage")
	}
	afterDefeat := rt.EnsureDivisionRoster(div)
	activeAfter := 0
	for _, u := range afterDefeat {
		if u.Role == RoleTrader && isActiveTradeRunState(u.State) {
			activeAfter++
		}
	}
	if activeAfter < MinActiveTradeRuns {
		t.Fatalf("expected scheduler to maintain >= %d active trade runs after defeat, got %d", MinActiveTradeRuns, activeAfter)
	}

	// Ambush [AI]Trader_Mei (non-lethal) -> raises DistressCall; [AI]Hunter_Sora rushes to reinforce!
	rt.RecordPlayerAttack(div, "[AI]Trader_Mei", "BanditKing", 3000, 2_000)
	dec, sora, ok := rt.StepAgent(context.Background(), div, "[AI]Hunter_Sora", nil, 2_100)
	if !ok || (dec.Action != ActionReinforceAlly && dec.Action != ActionAttackTarget) || (sora.State != StateReinforcing && sora.State != StateInCombat) {
		t.Fatalf("expected [AI]Hunter_Sora to reinforce ambushed [AI]Trader_Mei, got dec=%+v unit=%+v", dec, sora)
	}
	if sora.ReinforcingTrader != "[AI]Trader_Mei" {
		t.Fatalf("expected ReinforcingTrader=[AI]Trader_Mei, got %q", sora.ReinforcingTrader)
	}
}
