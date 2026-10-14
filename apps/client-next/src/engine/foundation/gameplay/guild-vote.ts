/*
===========================================================================

guild-vote.ts - an open guild vote's candidates and their ballots

The vote row asks for the candidates (0x7332) and 0xB332 brings them with
their ballots (82CEE0); each 0x3A6C type 4 then moves one ballot (82D0B0).
social.ts reads the wire and hands the values here; these keep the votes
of one guild.

===========================================================================
*/
import type { GuildVote, GuildVoteCandidate } from "./social-roster";

// The previous option of a first ballot (82D0B0 skips the decrement).
const NO_PREVIOUS_OPTION = 0xff;

/*
================
withVoteCandidates

0xB332: the vote's candidates and its time left. A vote the client does
not list is left alone (82CEE0 asserts on one).
================
*/
export function withVoteCandidates(
	votes: readonly GuildVote[],
	id: number,
	remainingMs: number,
	candidates: readonly GuildVoteCandidate[]
): readonly GuildVote[] {
	return votes.map( row => row.id === id ? { ...row, remainingMs, candidates } : row );
}

/*
================
withVoteBallot

0x3A6C type 4: the ballot leaves its previous option and its new option
now holds the pushed count.
================
*/
export function withVoteBallot(
	votes: readonly GuildVote[],
	id: number,
	previous: number,
	option: number,
	count: number
): readonly GuildVote[] {
	return votes.map( row =>
		row.id !== id || !row.candidates ? row : {
			...row,
			candidates: row.candidates.map( ( candidate, i ) =>
				i === option ?
					{ ...candidate, ballots: count } :
					i === previous && previous !== NO_PREVIOUS_OPTION ?
					{ ...candidate, ballots: Math.max( 0, candidate.ballots - 1 ) } :
					candidate
			)
		}
	);
}
