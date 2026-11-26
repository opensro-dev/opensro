/*
===========================================================================

config.go - AI caravan and thief agent configuration (port-only)

Reads the opt-in environment switches that control whether AI Caravan & Thief
agents populate the Jangan <-> Donwhang <-> Hotan trade highway and which
decision brain (hardcoded, llm, or hybrid) drives them.

The LLM brain's prompt carries nearby players' names and stats, and the owner
has kept it off pending a decision (#3704). Until then it may only reach a
model on the same host: an SRO_AGENT_LLM_URL that is not loopback is refused
and the agents run on the hardcoded brain (LoopbackURL). The SRO_AGENT_*
settings are not job variables (shardsettings), so no deployment sets them.

SRO_CARAVAN_HIRE_FEE is: the gold one AI agent's hire (and each hour's
extension) costs, a whole 1..MaxHireFeeGold, default DefaultHireFeeGold
(owner, 2026-11-26: a shard setting, the contributor's 1,500,000 as the
default). Anything else keeps the default and is reported in
Config.RefusedHireFee.

===========================================================================
*/
package caravanai

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

/*
================================================================================
Environment variable names and brain modes
================================================================================
*/

const (
	// EnvAICaravans gates the port-only AI caravan & thief agent system.
	// Unset or false leaves the shard 100% native.
	EnvAICaravans = "SRO_AI_CARAVANS"

	// EnvHireFee sets the AI agents' hire fee in gold (ParseHireFee).
	EnvHireFee = "SRO_CARAVAN_HIRE_FEE"

	// DefaultHireFeeGold is the hire fee when SRO_CARAVAN_HIRE_FEE is
	// unset or refused (owner, 2026-11-26).
	DefaultHireFeeGold int64 = 1_500_000

	// MaxHireFeeGold bounds the fee: a hire debits it from one purse,
	// which holds at most wire.MaxGold (config_test.go pins the two
	// equal, so config imports no item package).
	MaxHireFeeGold int64 = 100_000_000

	// EnvAgentBrain selects the agent decision engine:
	//   - "hardcoded" (default): deterministic utility & state-machine AI
	//   - "llm": OpenAI/Ollama-compatible HTTP chat completions with fallback
	//   - "hybrid": hardcoded frame combat + LLM tactical/social encounters
	EnvAgentBrain = "SRO_AGENT_BRAIN"

	// EnvAgentLLMURL sets the OpenAI/Ollama-compatible chat completions URL
	// (for example, "http://127.0.0.1:11434/v1/chat/completions").
	EnvAgentLLMURL = "SRO_AGENT_LLM_URL"

	// EnvAgentLLMModel sets the model identifier requested from the LLM
	// endpoint (defaults to "llama3.2:3b" when unset).
	EnvAgentLLMModel = "SRO_AGENT_LLM_MODEL"

	// EnvAgentLLMKey sets the optional Bearer token for hosted LLM APIs.
	EnvAgentLLMKey = "SRO_AGENT_LLM_KEY"

	// DefaultLLMModel is requested when SRO_AGENT_LLM_MODEL is unset.
	DefaultLLMModel = "llama3.2:3b"

	// DefaultLLMTimeout bounds any single LLM call before falling back to
	// the deterministic hardcoded brain.
	DefaultLLMTimeout = 1500 * time.Millisecond
)

// BrainMode identifies the decision engine driving AI caravan agents.
type BrainMode string

const (
	BrainHardcoded BrainMode = "hardcoded"
	BrainLLM       BrainMode = "llm"
	BrainHybrid    BrainMode = "hybrid"
)

// Config holds the resolved runtime configuration for AI caravan agents.
type Config struct {
	Terminals  map[TownID]Waypoint
	Highway    map[[2]TownID][]Waypoint
	Detours    map[[2]TownID][]Waypoint
	Routes     map[[2]TownID][][]Waypoint
	Enabled    bool
	BrainMode  BrainMode
	LLMURL     string
	LLMModel   string
	LLMKey     string
	LLMTimeout time.Duration
	// RefusedLLMURL is the SRO_AGENT_LLM_URL that ConfigFromEnv refused
	// as not loopback; BrainMode is then BrainHardcoded.
	RefusedLLMURL string
	// HireFeeGold is one hire's (and one extension's) fee; 0 means
	// DefaultHireFeeGold (Runtime.HireFee).
	HireFeeGold int64
	// RefusedHireFee is the SRO_CARAVAN_HIRE_FEE value ConfigFromEnv
	// refused; HireFeeGold is then the default.
	RefusedHireFee string
}

/*
==================
ParseHireFee

A whole number of gold 1..MaxHireFeeGold; ok is false for anything
else, unset included.
==================
*/
func ParseHireFee(raw string) (fee int64, ok bool) {
	fee, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || fee < 1 || fee > MaxHireFeeGold {
		return 0, false
	}
	return fee, true
}

/*
==================
LoopbackURL

Whether an LLM endpoint URL names this host: an http or https URL whose
host is "localhost" or a loopback IP literal (127.0.0.0/8, ::1). Any other
host could carry the prompt's player names off the machine.
==================
*/
func LoopbackURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

/*
==================
ParseBrainMode

ParseBrainMode normalizes a raw SRO_AGENT_BRAIN string into a known BrainMode,
defaulting to BrainHardcoded.
==================
*/
func ParseBrainMode(raw string) BrainMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(BrainLLM):
		return BrainLLM
	case string(BrainHybrid):
		return BrainHybrid
	default:
		return BrainHardcoded
	}
}

/*
==================
ConfigFromEnv

ConfigFromEnv reads the AI caravan agent configuration from process
environment variables.
==================
*/
func ConfigFromEnv() Config {
	enabled := parseEnvSwitch(os.Getenv(EnvAICaravans))
	mode := ParseBrainMode(os.Getenv(EnvAgentBrain))
	llmURL := strings.TrimSpace(os.Getenv(EnvAgentLLMURL))
	llmModel := strings.TrimSpace(os.Getenv(EnvAgentLLMModel))
	if llmModel == "" {
		llmModel = DefaultLLMModel
	}
	llmKey := strings.TrimSpace(os.Getenv(EnvAgentLLMKey))
	refused := ""
	if mode != BrainHardcoded && llmURL != "" && !LoopbackURL(llmURL) {
		refused, mode, llmURL = llmURL, BrainHardcoded, ""
	}
	fee, refusedFee := DefaultHireFeeGold, ""
	if raw := os.Getenv(EnvHireFee); strings.TrimSpace(raw) != "" {
		if parsed, ok := ParseHireFee(raw); ok {
			fee = parsed
		} else {
			refusedFee = raw
		}
	}
	return Config{
		HireFeeGold:    fee,
		RefusedHireFee: refusedFee,
		Enabled:        enabled,
		BrainMode:      mode,
		LLMURL:         llmURL,
		LLMModel:       llmModel,
		LLMKey:         llmKey,
		LLMTimeout:     DefaultLLMTimeout,
		RefusedLLMURL:  refused,
	}
}

func parseEnvSwitch(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}
