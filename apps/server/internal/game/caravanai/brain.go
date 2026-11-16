/*
===========================================================================

brain.go - Dual-mode AI Caravan & Thief decision engine (port-only)

Supports both a deterministic utility/state-machine brain (BrainHardcoded)
and an OpenAI/Ollama-compatible HTTP LLM brain (BrainLLM / BrainHybrid) that
evaluates tactical actions, personal grudge/ally dialogue, and memory notes
with automatic fallback to HardcodedBrain on timeout or error.

===========================================================================
*/
package caravanai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"opensro.online/server/internal/domain"
)

/*
================================================================================
Roles, actions, observations, and Brain interface
================================================================================
*/

// AgentRole mirrors the triangular Silkroad job roles (1=Trader, 2=Thief, 3=Hunter).
type AgentRole uint8

const (
	RoleTrader AgentRole = AgentRole(domain.JobTrader)
	RoleThief  AgentRole = AgentRole(domain.JobThief)
	RoleHunter AgentRole = AgentRole(domain.JobHunter)
)

// ActionKind identifies the tactical action chosen by an AI caravan agent.
type ActionKind string

const (
	ActionContinueRoute ActionKind = "CONTINUE_ROUTE"
	ActionAttackTarget  ActionKind = "ATTACK_TARGET"
	ActionUsePotion     ActionKind = "USE_POTION"
	ActionOfferParley   ActionKind = "OFFER_PARLEY"
	ActionRetreatToTown ActionKind = "RETREAT_TO_TOWN"
	ActionRecruitEscort ActionKind = "RECRUIT_ESCORT"
	ActionAssistAlly    ActionKind = "ASSIST_ALLY"
)

// ObservedPlayer summarizes a nearby player and the agent's remembered
// relationship with them.
type ObservedPlayer struct {
	Name       string
	JobType    uint8
	HPPercent  int
	Distance   float32
	TrustScore int
	Kills      int
	Deaths     int
	GoldStolen int64
	LastNote   string
}

// Situation is the snapshot fed into Brain.Decide on an agent's decision tick.
type Situation struct {
	AgentName    string
	Role         AgentRole
	Personality  Personality
	RegionID     uint16
	RegionHeat   int
	InTown       bool
	HPPercent    int
	MPPercent    int
	CargoStars   uint8
	CargoStacks  int
	UnderAttack  bool
	ParleyReady  bool
	NearbyAllies int
	Players      []ObservedPlayer
}

// Decision is the tactical action, optional target, in-character chat line,
// and updated memory note returned by a Brain.
type Decision struct {
	Action     ActionKind
	TargetName string
	Dialogue   string
	MemoryNote string
	Source     BrainMode
}

// Brain evaluates a Situation and returns a Decision.
type Brain interface {
	Decide(ctx context.Context, sit Situation) Decision
}

/*
================================================================================
HardcodedBrain - deterministic utility & grudge evaluator
================================================================================
*/

// HardcodedBrain implements the zero-latency deterministic decision engine.
type HardcodedBrain struct{}

/*
==================
NewHardcodedBrain

NewHardcodedBrain constructs the deterministic fallback and default brain.
==================
*/
func NewHardcodedBrain() *HardcodedBrain {
	return &HardcodedBrain{}
}

/*
==================
Decide

Decide evaluates survival thresholds, parley conditions, vendettas, trusted
allies, and triangular job combat rules.
==================
*/
func (h *HardcodedBrain) Decide(_ context.Context, sit Situation) Decision {
	// 1. Cornered AI Trader parley / cargo bribe check (< 35% HP, no allies).
	if sit.Role == RoleTrader &&
		sit.CargoStacks > 0 &&
		sit.UnderAttack &&
		sit.HPPercent > 0 &&
		sit.HPPercent < 35 &&
		sit.NearbyAllies == 0 &&
		sit.ParleyReady {
		thief := findPrimaryHostile(sit.Role, sit.Players)
		target := ""
		if thief != nil {
			target = thief.Name
		}
		return Decision{
			Action:     ActionOfferParley,
			TargetName: target,
			Dialogue:   formatParleyDialogue(sit.Personality, target),
			MemoryNote: "Offered cargo bribe under parley truce",
			Source:     BrainHardcoded,
		}
	}

	// 2. Emergency HP potion recovery when HP drops to 45% or below.
	if sit.HPPercent > 0 && sit.HPPercent <= 45 {
		return Decision{
			Action:   ActionUsePotion,
			Dialogue: "",
			Source:   BrainHardcoded,
		}
	}

	// 3. Town recruitment for 3★-5★ AI Traders before departing.
	if sit.InTown && sit.Role == RoleTrader && sit.CargoStars >= 3 && sit.NearbyAllies == 0 {
		for _, p := range sit.Players {
			if p.JobType == domain.JobHunter && p.TrustScore > domain.CaravanVendettaScore {
				return Decision{
					Action:     ActionRecruitEscort,
					TargetName: p.Name,
					Dialogue: fmt.Sprintf(
						"%s! I am hauling a %d-star caravan out of town. Type !accept %s to escort me for bonus gold!",
						p.Name, sit.CargoStars, sit.AgentName,
					),
					MemoryNote: "Offered mercenary escort contract in town",
					Source:     BrainHardcoded,
				}
			}
		}
	}

	// 4. Vendetta check: on-sight grudge callout and priority targeting.
	if vendetta := findVendettaTarget(sit.Role, sit.Players); vendetta != nil && !sit.InTown {
		return Decision{
			Action:     ActionAttackTarget,
			TargetName: vendetta.Name,
			Dialogue:   formatVendettaCallout(sit.Personality, *vendetta),
			MemoryNote: fmt.Sprintf("Engaged vendetta rival %s on sight", vendetta.Name),
			Source:     BrainHardcoded,
		}
	}

	// 5. Role-specific hostile engagement outside town.
	if !sit.InTown {
		if hostile := findPrimaryHostile(sit.Role, sit.Players); hostile != nil {
			if sit.Role == RoleTrader && !sit.UnderAttack && sit.Personality == PersonalityCautious {
				return Decision{
					Action:     ActionContinueRoute,
					TargetName: hostile.Name,
					Dialogue:   fmt.Sprintf("Stay sharp, %s is scouting near our caravan!", hostile.Name),
					Source:     BrainHardcoded,
				}
			}
			return Decision{
				Action:     ActionAttackTarget,
				TargetName: hostile.Name,
				Dialogue:   formatCombatCallout(sit.Role, sit.Personality, *hostile),
				MemoryNote: fmt.Sprintf("Fought %s in region %d", hostile.Name, sit.RegionID),
				Source:     BrainHardcoded,
			}
		}
	}

	// 6. Trusted ally greeting / support.
	if ally := findTrustedAlly(sit.Players); ally != nil {
		return Decision{
			Action:     ActionAssistAlly,
			TargetName: ally.Name,
			Dialogue:   fmt.Sprintf("Good to see a trusted friend on the Silkroad, %s! Watch the road ahead.", ally.Name),
			Source:     BrainHardcoded,
		}
	}

	return Decision{
		Action: ActionContinueRoute,
		Source: BrainHardcoded,
	}
}

func findVendettaTarget(role AgentRole, players []ObservedPlayer) *ObservedPlayer {
	var best *ObservedPlayer
	for i := range players {
		p := &players[i]
		if p.TrustScore > domain.CaravanVendettaScore {
			continue
		}
		if role == RoleTrader && p.JobType == domain.JobHunter {
			continue
		}
		if best == nil || p.TrustScore < best.TrustScore {
			best = p
		}
	}
	return best
}

func findPrimaryHostile(role AgentRole, players []ObservedPlayer) *ObservedPlayer {
	var best *ObservedPlayer
	for i := range players {
		p := &players[i]
		if !isNaturalHostile(role, p.JobType) {
			continue
		}
		if best == nil || p.TrustScore < best.TrustScore || p.Distance < best.Distance {
			best = p
		}
	}
	return best
}

func findTrustedAlly(players []ObservedPlayer) *ObservedPlayer {
	for i := range players {
		if players[i].TrustScore >= domain.CaravanAllyScore {
			return &players[i]
		}
	}
	return nil
}

func isNaturalHostile(role AgentRole, targetJob uint8) bool {
	switch role {
	case RoleTrader, RoleHunter:
		return targetJob == domain.JobThief
	case RoleThief:
		return targetJob == domain.JobTrader || targetJob == domain.JobHunter
	default:
		return false
	}
}

func formatParleyDialogue(p Personality, thiefName string) string {
	who := thiefName
	if who == "" {
		who = "friend"
	}
	switch p {
	case PersonalityGreedy:
		return fmt.Sprintf("Hold your blade, %s! Take 30%% of my silk stacks on the ground and let my bull pass!", who)
	case PersonalityCautious:
		return fmt.Sprintf("Truce, %s! I am dropping half my cargo right here—take the tribute and spare my caravan!", who)
	default:
		return fmt.Sprintf("Parley, %s! I've dropped part of my load on the road. Take the goods and give me 15 seconds to ride on!", who)
	}
}

func formatVendettaCallout(p Personality, rival ObservedPlayer) string {
	if rival.LastNote != "" && strings.Contains(strings.ToLower(rival.LastNote), "betrayed parley") {
		return fmt.Sprintf("%s! You broke our parley truce last time—no mercy today!", rival.Name)
	}
	if rival.GoldStolen > 0 {
		return fmt.Sprintf("%s! I still remember the %d gold you stole from me—time to settle the blood debt!", rival.Name, rival.GoldStolen)
	}
	if rival.Deaths > 0 {
		return fmt.Sprintf("There's %s! You've ambushed me %d time(s) before—not this run!", rival.Name, rival.Deaths)
	}
	return fmt.Sprintf("Our blood feud ends here, %s!", rival.Name)
}

func formatCombatCallout(role AgentRole, p Personality, target ObservedPlayer) string {
	switch role {
	case RoleThief:
		if p == PersonalityGreedy {
			return fmt.Sprintf("Hand over that specialty cargo, %s! The road belongs to the Bandit Den!", target.Name)
		}
		return fmt.Sprintf("Ambush! Drop the caravan packs, %s!", target.Name)
	case RoleHunter:
		return fmt.Sprintf("Back away from the caravan, thief %s! The Hunter Guild protects this road!", target.Name)
	default:
		return fmt.Sprintf("Defend the transport! %s is attacking the caravan!", target.Name)
	}
}

/*
================================================================================
LLMBrain - OpenAI / Ollama HTTP chat-completion brain with fallback
================================================================================
*/

// LLMBrain queries an OpenAI/Ollama-compatible /v1/chat/completions endpoint
// and automatically falls back to HardcodedBrain if the endpoint is unset,
// slow, or returns an unparseable response.
type LLMBrain struct {
	cfg      Config
	client   *http.Client
	fallback *HardcodedBrain
}

/*
==================
NewBrain

NewBrain returns a Brain configured according to cfg (HardcodedBrain when
BrainMode is BrainHardcoded or LLMURL is empty or not loopback, otherwise
LLMBrain with HardcodedBrain fallback). The loopback check is repeated here,
not only in ConfigFromEnv, so no Config built another way reaches a remote
endpoint (config.go).
==================
*/
func NewBrain(cfg Config) Brain {
	fallback := NewHardcodedBrain()
	if (cfg.BrainMode != BrainLLM && cfg.BrainMode != BrainHybrid) || !LoopbackURL(cfg.LLMURL) {
		return fallback
	}
	timeout := cfg.LLMTimeout
	if timeout <= 0 {
		timeout = DefaultLLMTimeout
	}
	return &LLMBrain{
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
			// Never follow a redirect: a 307/308 resends the POST body, so a
			// loopback endpoint could bounce the prompt and its player names
			// to another host. A redirect answer is not a decision, and the
			// fallback brain answers instead.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		fallback: fallback,
	}
}

type llmChatRequest struct {
	Model       string           `json:"model"`
	Temperature float64          `json:"temperature"`
	MaxTokens   int              `json:"max_tokens"`
	Messages    []llmChatMessage `json:"messages"`
}

type llmChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmChatResponse struct {
	Choices []struct {
		Message llmChatMessage `json:"message"`
	} `json:"choices"`
}

type llmDecisionPayload struct {
	Action     string `json:"action"`
	Target     string `json:"target"`
	Dialogue   string `json:"dialogue"`
	MemoryNote string `json:"memory_note"`
}

/*
==================
Decide

Decide evaluates sit via the configured LLM endpoint (or skips straight to
HardcodedBrain during routine non-encounter ticks in BrainHybrid mode). Any
timeout, HTTP error, or invalid JSON action falls back to HardcodedBrain.
==================
*/
func (l *LLMBrain) Decide(ctx context.Context, sit Situation) Decision {
	if l.cfg.BrainMode == BrainHybrid && len(sit.Players) == 0 && !sit.UnderAttack {
		return l.fallback.Decide(ctx, sit)
	}
	if strings.TrimSpace(l.cfg.LLMURL) == "" {
		return l.fallback.Decide(ctx, sit)
	}

	reqBody := llmChatRequest{
		Model:       l.cfg.LLMModel,
		Temperature: 0.3,
		MaxTokens:   140,
		Messages: []llmChatMessage{
			{
				Role: "system",
				Content: "You are a Silkroad Online job NPC agent on the Jangan-Donwhang-Hotan highway. " +
					"Respond ONLY with a single JSON object: " +
					`{"action":"CONTINUE_ROUTE|ATTACK_TARGET|USE_POTION|OFFER_PARLEY|RETREAT_TO_TOWN|RECRUIT_ESCORT|ASSIST_ALLY","target":"<player>","dialogue":"<short in-character line>","memory_note":"<short note>"}`,
			},
			{
				Role:    "user",
				Content: buildLLMPrompt(sit),
			},
		},
	}

	rawReq, err := json.Marshal(reqBody)
	if err != nil {
		return l.fallback.Decide(ctx, sit)
	}

	timeout := l.cfg.LLMTimeout
	if timeout <= 0 {
		timeout = DefaultLLMTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, l.cfg.LLMURL, bytes.NewReader(rawReq))
	if err != nil {
		return l.fallback.Decide(ctx, sit)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if l.cfg.LLMKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+l.cfg.LLMKey)
	}

	resp, err := l.client.Do(httpReq)
	if err != nil {
		return l.fallback.Decide(ctx, sit)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return l.fallback.Decide(ctx, sit)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return l.fallback.Decide(ctx, sit)
	}

	var chatResp llmChatResponse
	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil || len(chatResp.Choices) == 0 {
		return l.fallback.Decide(ctx, sit)
	}

	payload, ok := parseLLMDecisionJSON(chatResp.Choices[0].Message.Content)
	if !ok {
		return l.fallback.Decide(ctx, sit)
	}
	action, valid := validateActionKind(payload.Action)
	if !valid {
		return l.fallback.Decide(ctx, sit)
	}
	return Decision{
		Action:     action,
		TargetName: strings.TrimSpace(payload.Target),
		Dialogue:   domain.TruncateCaravanNote(strings.TrimSpace(payload.Dialogue)),
		MemoryNote: domain.TruncateCaravanNote(strings.TrimSpace(payload.MemoryNote)),
		Source:     BrainLLM,
	}
}

func buildLLMPrompt(sit Situation) string {
	var sb strings.Builder
	fmt.Fprintf(
		&sb,
		"agent=%s role=%d personality=%s region=%d heat=%d in_town=%v hp=%d%% mp=%d%% stars=%d cargo=%d under_attack=%v parley_ready=%v allies=%d",
		sit.AgentName, sit.Role, sit.Personality, sit.RegionID, sit.RegionHeat,
		sit.InTown, sit.HPPercent, sit.MPPercent, sit.CargoStars, sit.CargoStacks,
		sit.UnderAttack, sit.ParleyReady, sit.NearbyAllies,
	)
	for _, p := range sit.Players {
		fmt.Fprintf(
			&sb,
			"; player=%s job=%d hp=%d%% trust=%d kills=%d deaths=%d stolen=%d note=%q",
			p.Name, p.JobType, p.HPPercent, p.TrustScore, p.Kills, p.Deaths, p.GoldStolen, p.LastNote,
		)
	}
	return sb.String()
}

func parseLLMDecisionJSON(content string) (llmDecisionPayload, bool) {
	trimmed := strings.TrimSpace(content)
	start := strings.IndexByte(trimmed, '{')
	end := strings.LastIndexByte(trimmed, '}')
	if start < 0 || end <= start {
		return llmDecisionPayload{}, false
	}
	var out llmDecisionPayload
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &out); err != nil {
		return llmDecisionPayload{}, false
	}
	return out, true
}

func validateActionKind(raw string) (ActionKind, bool) {
	switch ActionKind(strings.ToUpper(strings.TrimSpace(raw))) {
	case ActionContinueRoute:
		return ActionContinueRoute, true
	case ActionAttackTarget:
		return ActionAttackTarget, true
	case ActionUsePotion:
		return ActionUsePotion, true
	case ActionOfferParley:
		return ActionOfferParley, true
	case ActionRetreatToTown:
		return ActionRetreatToTown, true
	case ActionRecruitEscort:
		return ActionRecruitEscort, true
	case ActionAssistAlly:
		return ActionAssistAlly, true
	default:
		return "", false
	}
}
