package guild

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
)

// GpDonateOutcome is one handled 0x740F request. AckPayload is the
// 0xB40F {u8 1}{u32 amount} answer for the DONATING ACTOR ONLY (the
// second-host guide line); GuildGpPushPayload is the 0x3B29 subOp-5
// &0x08 guild-GP delta and DonorGpPushPayload the subOp-6 &0x08
// donated-GP delta, both for every ONLINE member named in MemberNames -
// the FULL member list INCLUDING the actor (both deltas are silent data
// writes on the client and the ack carries no state, so excluding the
// donor would leave their own pane stale - the notice-edit inclusion
// rationale, DECISION). ErrorPayload is the 0xB40F {u8 2}{u8 code}
// refusal for the actor when the v1.188 server names one
// (donateRefusalCode); the native client has no SP check of its own
// (CIFGuildPointUp_OnCreate 5F4B00 only shows the SP, and
// CIFGuildPointUp_OnConfirm 5F49F0 sends the typed amount unchecked),
// so this answer is the player's only feedback.
type GpDonateOutcome struct {
	AckPayload         []byte
	ErrorPayload       []byte
	GuildGpPushPayload []byte
	DonorGpPushPayload []byte
	MemberNames        []string
	Refusal            string
}

func refusedGpDonate(reason string) GpDonateOutcome {
	return GpDonateOutcome{Refusal: reason}
}

/*
================
donateRefusalCode

The v1.188 code for a donation-door refusal (errors.go
GuildErrDonate*), or false for the refusals the native server has no
code for (overflow, a torn roster), which stay silent.
================
*/
func donateRefusalCode(refusal enterworld.GuildRefusal) (byte, bool) {
	switch refusal {
	case enterworld.GuildRefusalInvalidAmount:
		return GuildErrDonateInvalidAmount, true
	case enterworld.GuildRefusalNotMember:
		return GuildErrDonateNotMember, true
	case enterworld.GuildRefusalInsufficientPoints:
		return GuildErrDonateSPDeficit, true
	}
	return 0, false
}

// HandleGpDonate applies one decoded 0x740F request through the ATOMIC
// DonateGuildPoints store door (SP debit + guild-GP credit + member
// DonatedGP credit, ONE commit - two sequential doors would tear SP-vs-
// GP state on a crash between commits). The arm is IN-GUILD ONLY: the
// client's donate button gates solely on CCharacterDependentData_IsInGuild (@0x005e4db8) with
// NO permission-mask bit and NO grade gate, so any member may donate.
// Validation order mirrors the v1.188 job posture (identity -> state ->
// resource last; EVIDENCE(v1.188, logic) - the 0x4c0c gold-deficit
// family checks resources after resolving both parties): the amount
// floor (>= 1), the SP sufficiency and the u32 wrap guards all
// re-validate INSIDE the door under the store lock.
func HandleGpDonate(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte) GpDonateOutcome {
	if actor == nil {
		return refusedGpDonate("characterNotFound")
	}
	actor = characterSnapshot(deps, divisionID, actor)
	if actor == nil {
		return refusedGpDonate("characterNotFound")
	}
	if actor.DeletePending {
		return refusedGpDonate("deletePending")
	}
	if deps.GuildAuthority() == nil {
		return refusedGpDonate("no guild store wired")
	}
	request, err := DecodeGpDonateRequest(payload)
	if err != nil {
		return refusedGpDonate(err.Error())
	}
	donation, refusal := deps.GuildAuthority().DonateGuildPoints(
		divisionID,
		actor.ID,
		request.Amount,
	)
	if refusal.Refused() {
		outcome := refusedGpDonate(fmt.Sprintf(
			"the donation door refused amount %d (%s; re-validated under the store lock)",
			request.Amount,
			guildRefusalReason(refusal),
		))
		if code, named := donateRefusalCode(refusal); named {
			outcome.ErrorPayload = EncodeGuildErrorResult(code)
		}
		return outcome
	}

	names := make([]string, 0, len(donation.Snapshot.Members))
	for _, member := range donation.Snapshot.Members {
		names = append(names, member.Name)
	}
	return GpDonateOutcome{
		AckPayload:         EncodeGpDonateAckB40F(request.Amount),
		GuildGpPushPayload: EncodeGuildGp3B29(donation.Snapshot.Guild.GP),
		DonorGpPushPayload: EncodeMemberDonatedGp3B29(donation.Donor.JID, donation.Donor.DonatedGP),
		MemberNames:        names,
	}
}
