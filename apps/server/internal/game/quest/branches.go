/*
===========================================================================

branches.go - offers whose reply chooses how the quest is done

Rahid 2 (CQNO_RM_OLDWOMAN_2, talk 89E670) offers _01 with three replies:
collect 200 Rocky Feathers (_02), wait two game months (_06) or refuse.
The reply picks the record's branch for good: its own not-achieved,
completion and achieved-now lines, its reward, and for a waiting branch an
online-minute countdown (89E050) that replaces the objective.

===========================================================================
*/
package quest

import (
	"fmt"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// branchSeparator joins a quest codename and its chosen branch in the
// acceptance token the NPC dialogue carries.
const branchSeparator = "~"

/*
================
OfferBranch

One reply of a branching offer. Empty symbols keep the quest's own.
================
*/
type OfferBranch struct {
	ReplySymbol          string
	AcceptResponseSymbol string
	NotAchievedSymbol    string
	CompletePromptSymbol string
	AchievedNowSymbol    string
	// WaitMinutes, when set, replaces the objective: the branch is achieved
	// after this many admitted online minutes (89E670 sets 0x708).
	WaitMinutes uint16
	// NoReward pays nothing (v1.150 popup: "[Option 2] Wait 2 months None").
	NoReward bool
}

/*
================
branchToken
================
*/
func branchToken(code string, branch int) string {
	return code + branchSeparator + strconv.Itoa(branch)
}

/*
================
parseBranchToken
================
*/
func parseBranchToken(token string) (string, int, bool) {
	code, suffix, found := strings.Cut(token, branchSeparator)
	if !found {
		return token, 0, false
	}
	n, err := strconv.Atoi(suffix)
	if err != nil || n < 0 {
		return token, 0, false
	}
	return code, n, true
}

/*
================
validateOfferBranches

A branching offer is an ordinary unstaged NPC quest with reply symbols.
================
*/
func validateOfferBranches(spec QuestSpec) error {
	if len(spec.OfferBranches) == 0 {
		return nil
	}
	if len(spec.OfferBranches) > 250 || len(spec.Stages) > 0 || spec.KindByte == 2 || spec.EndNpcCodename == "" {
		return fmt.Errorf("quest %s invalid offer branch contract", spec.Codename)
	}
	for _, branch := range spec.OfferBranches {
		if branch.ReplySymbol == "" || branch.WaitMinutes > 0 && branch.AchievedNowSymbol == "" {
			return fmt.Errorf("quest %s invalid offer branch contract", spec.Codename)
		}
	}
	return nil
}

/*
================
recordBranch

The branch an active record chose, if the quest branches.
================
*/
func recordBranch(def *Definition, record enterworld.ActiveQuestRecord) (OfferBranch, bool) {
	if int(record.Branch) >= len(def.OfferBranches) {
		return OfferBranch{}, false
	}
	return def.OfferBranches[record.Branch], true
}

/*
================
waitingBranch

Whether the record waits instead of pursuing the quest's objective.
================
*/
func waitingBranch(def *Definition, record enterworld.ActiveQuestRecord) bool {
	branch, ok := recordBranch(def, record)
	return ok && branch.WaitMinutes > 0
}

/*
================
branchDefinition

The definition as the record's branch sees it: its lines and reward.
================
*/
func branchDefinition(def *Definition, record enterworld.ActiveQuestRecord) *Definition {
	branch, ok := recordBranch(def, record)
	if !ok {
		return def
	}
	d := *def
	if branch.NotAchievedSymbol != "" {
		d.NotAchievedSymbol = branch.NotAchievedSymbol
	}
	if branch.CompletePromptSymbol != "" {
		d.CompletePromptSymbol = branch.CompletePromptSymbol
	}
	if branch.AchievedNowSymbol != "" {
		d.AchievedNowSymbol = branch.AchievedNowSymbol
	}
	if branch.NoReward {
		d.RewardExp, d.RewardGold, d.RewardSkillExp = 0, 0, 0
		d.RewardItems, d.RewardChoices = nil, nil
	}
	return &d
}

/*
================
offerBranchRows

The reply rows of a branching offer, each carrying its acceptance token.
================
*/
func offerBranchRows(def *Definition) []NpcBranch {
	out := make([]NpcBranch, 0, len(def.OfferBranches))
	for i, branch := range def.OfferBranches {
		out = append(out, NpcBranch{Codename: branchToken(def.Codename, i), ReplySymbol: branch.ReplySymbol,
			AcceptResponseSymbol: branch.AcceptResponseSymbol})
	}
	return out
}

/*
================
NpcBranch

One reply row of a branching offer as the NPC dialogue presents it.
================
*/
type NpcBranch struct {
	Codename, ReplySymbol, AcceptResponseSymbol string
}
