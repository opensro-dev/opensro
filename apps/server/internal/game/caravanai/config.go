/*
===========================================================================

config.go - AI caravan and thief agent configuration (port-only)

Reads the opt-in environment switches that control whether AI Caravan & Thief
agents populate the Jangan <-> Donwhang <-> Hotan trade highway and which
decision brain (hardcoded, llm, or hybrid) drives them.

===========================================================================
*/
package caravanai

import (
	"os"
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
	Enabled    bool
	BrainMode  BrainMode
	LLMURL     string
	LLMModel   string
	LLMKey     string
	LLMTimeout time.Duration
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
	return Config{
		Enabled:    enabled,
		BrainMode:  mode,
		LLMURL:     llmURL,
		LLMModel:   llmModel,
		LLMKey:     llmKey,
		LLMTimeout: DefaultLLMTimeout,
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
