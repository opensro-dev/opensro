/*
===========================================================================

social-guild-union.ts - guild, union and guild-war packet lane

Owns the guild half of the social wire: the guild record and member
updates (0x32C4, 0x3B29), the master vote lane (0x3A6C, 0xB332), the
war roster and war sub-ops (0x32BB, 0x3B29), the alliance rows
(0x341E, 0x3B29) and the guild and union answer notices. social.ts
lends the lane its frame cursor and publishes the state it returns;
party and invitation frames never reach here. The native addresses
on the arms are the handlers the printers follow.

===========================================================================
*/
import { guildWarProposalReply } from "./guild-war";
import { withVoteBallot, withVoteCandidates } from "./guild-vote";
import { resolveNativeNotice, type NativeNoticeContext } from "./native-notice";
import type { SystemNotice } from "./system-notices";
import { assertUniqueRosterRows, type Guild, type GuildMember, type GuildWar, type SocialState } from "./social-roster";

// The fortress position title a 0xB65F role bit carries, by bit.
// overhead-layout.ts keeps its own inline copy of this map for
// member titles; the values agree, and a rename moves both by hand.
const POSITION_TITLES: Readonly<Record<number, string>> = {
	1: "UIIT_STT_FORT_GUILD_COMMANDER",
	2: "UIIT_STT_FORT_GUILD_SUBCOMMANDER",
	4: "UIIT_STT_FORT_GUILD_BATTLEMANAGER",
	8: "UIIT_STT_FORT_GUILD_PRODUCTMANAGER",
	16: "UIIT_STT_FORT_GUILD_TRAINERMANAGER",
	32: "UIIT_STT_FORT_GUILD_ENGINEER"
} as const;

/*
================
SocialGuildLane

The frame social.ts lends this lane: the state the frame started
from, the state built so far, the opcode and its first payload
byte, and the cursor readers over the frame bytes. skipRest
consumes the rest of the frame, for the sub-ops the native handler
folds onto its default block. The lane returns the next state, or
null when the opcode is not a guild or union one; on null the
cursor is untouched.
================
*/
export interface SocialGuildLane {
	readonly state: SocialState;
	readonly next: SocialState;
	readonly op: number;
	readonly lead: number;
	readonly noticeContext: NativeNoticeContext & { readonly now?: number; };
	readonly u8: () => number;
	readonly u32: () => number;
	readonly str: () => string;
	readonly skipRest: () => void;
}

/*
================
socialGuildUnionPacket

Decode one guild, union or guild-war frame through the lent cursor.
Only evidenced server-emitted arms enter this projection, as in
socialPacket.
================
*/
export function socialGuildUnionPacket( lane: SocialGuildLane ): SocialState | null {
	if (
		![
			0x37d4,
			0x34f3,
			0x32bb,
			0x341e,
			0xb71b,
			0xb465,
			0x32c4,
			0x3a6c,
			0x3b29,
			0xb332,
			0xb663,
			0xb515,
			0xb6dc,
			0xb330,
			0xb322,
			0xb7d4,
			0xb140,
			0xb3f7,
			0xb77a,
			0xb56e,
			0xb66e,
			0xb40f,
			0xb2bc,
			0xb65f,
			// Union invite/leave/expel and the rights grant answer
			// [1] or [2][code] as category 0x10 notices (75CCA0, 75CCF0,
			// 75CD40, 75CB80).
			0xb379,
			0xb795,
			0xb680,
			0xb44e,
			0xb3f0
		].includes( lane.op )
	) {
		return null;
	}
	const state = lane.state, op = lane.op, lead = lane.lead, noticeContext = lane.noticeContext;
	const u8 = lane.u8, u32 = lane.u32, str = lane.str, skipRest = lane.skipRest;
	let next = lane.next;
	if ( op === 0x37d4 ) {
		u32();
		const name = str(),
			role = u8(),
			roleUpdates = [ ...(state.roleUpdates ?? []).filter( r => r.name !== name ), { name, role } ];
		if ( roleUpdates.length > 4096 ) throw Error( "Role update capacity" );
		next = {
			...next,
			roleUpdates,
			guild: next.guild ?
				{ ...next.guild, members: next.guild.members.map( m => m.name === name ? { ...m, role } : m ) } :
				null
		};
	} else if ( op === 0x34f3 ) {
		const type = u8(), updates = [ ...(state.crestUpdates ?? []) ];
		/*
		================
		update
		================
		*/
		function update( row: NonNullable<SocialState["crestUpdates"]>[number] ) {
			const i = updates.findIndex( r => r.name === row.name );
			if ( i < 0 ) updates.push( row );
			else updates[i] = { ...updates[i], ...row };
			if ( updates.length > 4096 ) throw Error( "Crest update capacity" );
		}
		if ( type === 1 ) {
			const guildId = u32(), name = str(), crest = u32();
			if ( crest !== 0xffffffff ) update( { name, guildId, crest } );
			if ( next.guild?.name === name ) next = { ...next, guild: { ...next.guild, crest } };
		} else if ( type === 2 ) {
			const allianceId = u32(), count = u8(), names = Array.from( { length: count }, str ), allianceCrest = u32();
			if ( allianceCrest !== 0xffffffff ) {
				for ( const name of names ) update( { name, allianceId, allianceCrest } );
			}
			if ( next.allianceCrests?.[0] === allianceId ) {
				next = { ...next, allianceCrests: [ allianceId, allianceCrest ] };
			}
		} else throw Error( "Unsupported crest update" );
		next = { ...next, crestUpdates: updates };
	} else if ( op === 0x32bb ) {
		let wars: readonly GuildWar[] = state.wars ?? [];
		const count = u8();
		for ( let i = 0; i < count; i++ ) wars = mergeWar( wars, readWar( lane ) );
		next = { ...next, wars };
	} else if ( op === 0x341e ) {
		const allianceCrests = [ u32(), u32() ] as const, allianceMaster = u32(), count = u8();
		const alliances = [ ...(state.alliances ?? []) ];
		for ( let i = 0; i < count; i++ ) {
			const row = { id: u32(), name: str(), level: u8(), master: str(), model: u32(), flags: u8() };
			const at = alliances.findIndex( g => g.id === row.id );
			if ( at < 0 ) alliances.push( row );
			else alliances[at] = row;
		}
		assertUniqueRosterRows( alliances, 255 );
		next = { ...next, alliances, allianceMaster, allianceCrests };
	} else if ( op === 0xb71b || op === 0xb465 ) {
		if ( op === 0xb71b ) next = { ...next, warPending: 0 };
		if ( u8() === 2 ) {
			const resolution = resolveNativeNotice( 0x10, u8(), noticeContext );
			next = { ...next, notice: resolution.kind === "notice" ? resolution.notice : undefined };
		}
	} else if ( op === 0x32c4 ) {
		next = { ...next, guild: guildBlock( lane ) };
	} else if ( op === 0x3a6c ) {
		// 7603D0: 1 opens a vote, 3 closes it (1 elected, 2 broken), 4 moves
		// a ballot, 5 restates the time; 2 is an assert in v1.150.
		const type = u8(), id = u32(), guild = next.guild;
		const votes = guild?.votes ?? [];
		if ( type === 1 ) {
			const vote = { id, kind: u8(), remainingMs: u32() };
			if ( guild ) {
				next = {
					...next,
					guild: { ...guild, votes: [ ...votes.filter( row => row.id !== id ), vote ] }
				};
			}
		} else if ( type === 3 ) {
			const result = u8();
			if ( result === 1 ) {
				const heir = u32(), name = guild?.members.find( m => m.id === heir )?.name ?? "";
				next = {
					...next,
					notice: {
						key: "UIIT_MSG_MRELEASE_BEELETED",
						value: 0,
						nativeType: 0,
						arguments: [ name, guild?.name ?? "" ]
					}
				};
			} else if ( result === 2 ) {
				next = { ...next, notice: { key: "UIIT_MSG_MRELEASE_BROKEN", value: 0, nativeType: 0 } };
			} else {
				throw Error( "Invalid guild vote result" );
			}
			if ( guild ) next = { ...next, guild: { ...guild, votes: votes.filter( row => row.id !== id ) } };
		} else if ( type === 4 ) {
			// 82D0B0: the ballot leaves its previous option (0xFF: none) and
			// its new option now holds the pushed count.
			const previous = u8(), option = u8(), count = u8();
			if ( guild ) {
				next = { ...next, guild: { ...guild, votes: withVoteBallot( votes, id, previous, option, count ) } };
			}
		} else if ( type === 5 ) {
			const remainingMs = u32();
			if ( guild ) {
				next = {
					...next,
					guild: { ...guild, votes: votes.map( row => row.id === id ? { ...row, remainingMs } : row ) }
				};
			}
		} else {
			throw Error( "Unsupported guild vote update" );
		}
	} else if ( op === 0x3b29 ) {
		const type = u8();
		// 762040 dispatches through the byte table at 7637DC. Slot 0 only
		// refreshes the guild window, and these subOps fold onto the default
		// block at 763764, which returns without touching the stream.
		if ( type === 0 || guildUpdateIgnored( type ) ) skipRest();
		else if ( type === 0x32 ) {
			// 76309C: 0 refuses the proposal, 2 times a pending one out, and 3
			// opens the two-line native suggestion dialog (7632F5).
			const result = u8();
			next = guildWarProposalReply( next, result, result === 0 || result === 2 ? str() : "" );
			// Every other reply, including the suggestion dialog, reads no body.
		} else if ( type === 1 ) {
			// 762040 case 1: the break prints the guild name on the guide
			// (67a600 @76213a) and the announcement plate (67a140 @762168)
			// before the block drops, so both channels ride one notice.
			next = {
				...next,
				notice: {
					key: "UIIT_MSG_GUILD_BREAK_RESULT",
					value: 0,
					nativeType: 0,
					notificationBanner: true,
					arguments: [ next.guild?.name ?? "" ]
				},
				guild: null,
				alliances: [],
				allianceMaster: 0,
				allianceCrests: undefined,
				wars: [],
				warCountdown: undefined,
				warResult: undefined
			};
		} else if ( !next.guild ) {
			// 762040 never asks whether the player is in a guild: every arm
			// writes through CCharacterDependentData_GetGuildData, a member of
			// the character data that always exists, and the full guild record
			// that arrives later overwrites it. An update before that record
			// (a member joining while this character enters) is consumed and
			// changes nothing here, instead of failing the session.
			skipRest();
		} else {
			let guild = next.guild;
			if ( type === 0x0d ) {
				const row = { id: u32(), name: str(), level: u8(), master: str(), model: u32(), flags: u8() };
				const alliances = [ ...(next.alliances ?? []).filter( a => a.id !== row.id ), row ];
				assertUniqueRosterRows( alliances, 255 );
				// 5FAFF0 @5fb075: a joined ally announces itself on the guide
				// (67a600 @5fb0a8) with its guild name.
				next = {
					...next,
					alliances,
					notice: {
						key: "UIIT_MSG_GUILD_ALLY_JOIN_RESULT",
						value: 0,
						nativeType: 0,
						arguments: [ row.name ]
					}
				};
			} else if ( type === 0x0e ) {
				const mask = u8(), id = u32();
				if ( mask & ~15 ) throw Error( "Unsupported alliance mask" );
				const patch = {
					...(mask & 1 ? { name: str() } : {}),
					...(mask & 2 ? { level: u8() } : {}),
					...(mask & 4 ? { master: str(), model: u32() } : {}),
					...(mask & 8 ? { flags: u8() } : {})
				};
				next = { ...next, alliances: next.alliances?.map( a => a.id === id ? { ...a, ...patch } : a ) };
			} else if ( type === 0x12 ) {
				const mode = u8();
				if ( ![ 1, 2, 3 ].includes( mode ) ) throw Error( "Unsupported alliance removal" );
				const id = mode === 3 ? 0 : u32();
				const clear = mode === 3 || id === guild.id || (next.alliances?.length ?? 0) < 2;
				const removed = (next.alliances ?? []).filter( a => clear || a.id === id ),
					crestUpdates = [ ...(next.crestUpdates ?? []) ];
				for ( const row of removed ) {
					const at = crestUpdates.findIndex( c => c.name === row.name ),
						patch = {
							...(at < 0 ? {} : crestUpdates[at]),
							name: row.name,
							allianceId: 0,
							allianceCrest: 0
						};
					if ( at < 0 ) crestUpdates.push( patch );
					else crestUpdates[at] = patch;
				}
				// 762040 slot 8 (@762cb5): mode 1 prints the exit line and
				// mode 2 the expel line, each with the removed guild's name
				// (guide 67a600 @762de4/@762efb); mode 3 prints the breakup
				// line (@762f47, a guide line, not a box) as the alliance
				// clears. An id the roster cannot name prints nothing -
				// the native resolves the name from its ally record.
				const named = (next.alliances ?? []).find( a => a.id === id )?.name ??
					(id === guild.id ? guild.name : undefined);
				next = {
					...next,
					crestUpdates,
					alliances: clear ? [] : next.alliances?.filter( a => a.id !== id ),
					...(clear ? { allianceCrests: undefined, allianceMaster: 0 } : {}),
					...(mode === 3 ?
						{
							notice: {
								key: "UIIT_MSG_QUESTION_GUILD_RESPECT_ALLY_BREAKUP",
								value: 0,
								nativeType: 0
							}
						} :
						named !== undefined ?
						{
							notice: {
								key: mode === 1 ?
									"UIIT_MSG_GUILD_ALLY_EXIT_RESULT" :
									"UIIT_MSG_GUILD_RESPECT_ALLY_EXPEL_RESULT",
								value: 0,
								nativeType: 0,
								arguments: [ named ]
							}
						} :
						{})
				};
			} else if ( type === 0x19 ) {
				const row = readWar( lane );
				next = {
					...next,
					wars: mergeWar( next.wars ?? [], row ),
					...(row ?
						{
							notice: {
								key: "UIIT_MSG_GUILDWAR_GOTOWAR",
								value: 0,
								arguments: [ guild.name, row.name ],
								notificationBanner: true,
								bannerOnly: true
							}
						} :
						{})
				};
			} else if ( type === 0x1a || type === 0x1b ) {
				const id = u32();
				const row = next.wars?.find( w => w.id === id );
				next = {
					...next,
					wars: next.wars?.map( w => w.id === id ? { ...w, ending: true } : w ),
					...(row ?
						{
							warCountdown: { remaining: 60, nextAt: (noticeContext.now ?? 0) + 1000 },
							warResult: {
								sequence: (state.warResult?.sequence ?? 0) + 1,
								key: "UIIT_CTL_GUILDWAR_ENDCOUNT",
								names: [ guild.name, row.name ]
							},
							notice: {
								key: "UIIT_MSG_GUILDWAR_END_COUNTDOWN",
								value: 60,
								notificationBanner: true,
								bannerOnly: true
							}
						} :
						{})
				};
			} else if ( type === 0x1c ) {
				const id = u32();
				const winner = u32(), row = next.wars?.find( w => w.id === id );
				next = {
					...next,
					wars: next.wars?.filter( w => w.id !== id ),
					warCountdown: undefined,
					...(row ?
						{
							warResult: {
								sequence: (state.warResult?.sequence ?? 0) + 1,
								key: winner === guild.id ?
									"UIIT_MSG_GUILDWAR_WINERGUILD" :
									"UIIT_MSG_GUILDWAR_LOSEGUILD",
								names: [ row.name ]
							}
						} :
						{})
				};
			} else if ( type === 0x1d ) {
				const mode = u8(), id = u32(), delta = u32(), memberId = u32();
				const victim = str() || str();
				const killer = guild.members.find( m => m.id === memberId );
				guild = {
					...guild,
					members: guild.members.map( m =>
						m.id !== memberId ?
							m :
							mode === 1 ?
							{
								...m,
								warScore: ((m.warScore ?? 0) + delta) >>> 0,
								warKills: ((m.warKills ?? 0) + 1) >>> 0
							} :
							mode === 2 ?
							{ ...m, warDeaths: ((m.warDeaths ?? 0) + 1) >>> 0 } :
							m
					)
				};
				next = {
					...next,
					wars: next.wars?.map( w =>
						w.id !== id ?
							w :
							mode === 1 ?
							{ ...w, localScore: (w.localScore + delta) >>> 0 } :
							mode === 2 ?
							{ ...w, enemyScore: (w.enemyScore + delta) >>> 0 } :
							w
					),
					// 61A500 @61a6f3: modes 1 and 2 raise the kill on the
					// centre banner (67d030 @61a773) - the member the roster
					// resolves by jid, then the victim the wire names,
					// passed again for the belonging slot (@61a710).
					...((mode === 1 || mode === 2) && killer ?
						{
							notice: {
								key: "UIIT_MSG_GUILDWAR_KILLNEWS",
								value: 0,
								notificationBanner: true,
								bannerOnly: true,
								arguments: [ killer.name, victim, victim ]
							}
						} :
						{})
				};
			} else if ( type === 0x1f ) {
				const id = u32(), word3c = u32();
				next = {
					...next,
					wars: next.wars?.map( w => w.id === id ? { ...w, word3c, clockAt: noticeContext.now ?? 0 } : w )
				};
			} else if ( type === 0x23 ) {
				const enemyId = u32(), name = str();
				next = { ...next, wars: next.wars?.map( w => w.enemyId === enemyId ? { ...w, name } : w ) };
			} else if ( type === 2 ) {
				const row = guildMember( lane );
				const members = [ ...guild.members, row ];
				assertUniqueRosterRows( members, 250 );
				guild = { ...guild, members };
				// 5F3340 @5f34ce: the join line prints for the member whose
				// name the join event just cached (762040 @76239a writes it
				// via 8323F0 before the insert; the repaint's compare
				// @5f347e hits and prints @5f353a). The local player's own
				// row never goes through that event - it arrives with the
				// cold roster - so a self append stays silent, and a later
				// online flip prints CONNECT on the type-6 arm instead.
				if ( row.offline === 0 && row.name !== next.localName ) {
					next = {
						...next,
						notice: {
							key: "UIIT_MSG_GUILD_JOIN_RESULT",
							value: 0,
							nativeType: 0,
							arguments: [ row.name ]
						}
					};
				}
			} else if ( type === 3 ) {
				const id = u32(), kind = u8();
				const removed = guild.members.find( m => m.id === id );
				// 762040 slot 3 (@76251d): kind 1 prints the exit line only
				// on the leaver's own client, formatted with the guild name;
				// kind 2 prints the expulsion line with the member name for
				// every recipient (guide 67a600 @7626b7); other kinds are
				// silent. The self test is the native name compare (516aa0).
				if ( kind === 1 && removed?.name === next.localName ) {
					next = {
						...next,
						notice: {
							key: "UIIT_MSG_GUILD_EXIT_RESULT",
							value: 0,
							nativeType: 0,
							arguments: [ guild.name ]
						}
					};
				} else if ( kind === 2 && removed ) {
					next = {
						...next,
						notice: {
							key: "UIIT_MSG_GUILD_EXPULSION_RESULT",
							value: 0,
							nativeType: 0,
							arguments: [ removed.name ]
						}
					};
				}
				if ( removed?.name === next.localName ) {
					next = {
						...next,
						guild: null,
						alliances: [],
						allianceMaster: 0,
						allianceCrests: undefined,
						wars: [],
						warCountdown: undefined,
						warResult: undefined
					};
				}
				guild = { ...guild, members: guild.members.filter( m => m.id !== id ) };
			} else if ( type === 5 ) {
				const mask = u8();
				if ( mask & ~0x5e ) {
					throw Error( "Unsupported guild info mask" );
				}
				if ( mask & 2 ) guild = { ...guild, name: str() };
				if ( mask & 4 ) {
					// 5E4710: the new level announces itself in chat.
					guild = { ...guild, level: u8() };
					next = {
						...next,
						notice: {
							key: "UIIT_MSG_GUILD_LEVEL_UP_RESULT",
							value: guild.level,
							nativeType: 0,
							arguments: [ String( guild.level ) ]
						}
					};
				}
				if ( mask & 8 ) {
					guild = { ...guild, gp: u32() };
				}
				if ( mask & 16 ) {
					guild = { ...guild, subject: str(), contents: str() };
					// 5E4710 @5e4a33: a broadcast notice update also raises
					// the centre banner (67d030).
					next = {
						...next,
						notice: {
							key: "UIIT_MSG_GUILD_COMMON_KNOW_REMIND_UPDATE",
							value: 0,
							notificationBanner: true,
							bannerOnly: true
						}
					};
				}
				if ( mask & 64 ) {
					const flags = u8();
					guild = { ...guild, flags: flags ? (guild.flags ?? 0) | flags : 0 };
					next = { ...next, soldierAttributeSequence: (next.soldierAttributeSequence ?? 0) + 1 };
				}
			} else if ( type === 0x14 ) {
				const count = u8(), updates = new Map<number, number>();
				for ( let i = 0; i < count; i++ ) {
					const id = u32();
					updates.set( id, u32() );
				}
				guild = {
					...guild,
					members: guild.members.map( m =>
						updates.has( m.id ) ? { ...m, permissions: updates.get( m.id )! } : m
					)
				};
			} else if ( type === 6 || type === 0x16 ) {
				const count = type === 6 ? 1 : u8(), commonMask = type === 6 ? undefined : u8();
				const flips: SystemNotice[] = [];
				for ( let i = 0; i < count; i++ ) {
					const id = u32(), mask = commonMask ?? u8(), old = guild.members.find( m => m.id === id );
					if ( !old || mask & ~0x7f ) throw Error( "Unsupported guild member delta" );
					// 5E9D20 @5e9d90: the applicator order is &1 level (+0x24),
					// &2 offline (+0x26), &4 grade (+0x25), then donated,
					// permissions, name, role. (The &1/&2 legs stood swapped
					// here until #2281; both are u8 reads, so only the field
					// assignment was wrong.)
					const patch = {
						...(mask & 1 ? { level: u8() } : {}),
						...(mask & 2 ? { offline: u8() } : {}),
						...(mask & 4 ? { grade: u8() } : {}),
						...(mask & 8 ? { donated: u32() } : {}),
						...(mask & 16 ? { permissions: u32() } : {}),
						...(mask & 32 ? { name: str() } : {}),
						...(mask & 64 ? { role: u8() } : {})
					};
					// 5F3340: the announcement block is gated on the offline
					// leg (mask &2 @5f3392) and prints only on an actual
					// flip; the native row latch (+0x374, set @5f36d1 and
					// cleared by the disconnect arm @5f38e0) collapses to
					// the transition itself in this state model.
					if ( (mask & 2) && patch.offline !== undefined && patch.offline !== old.offline ) {
						const self = old.name === next.localName;
						if ( patch.offline === 0 ) {
							if ( self ) {
								// @5f3687: the self leg prints the guild notice
								// text when one exists - the object at record
								// +0x50 (buffer +0x54, size +0x64), which the
								// 5E4710 write arm fills from wire string #1
								// @5e4a64: the port's subject. Contents is
								// wire string #2, the window body.
								if ( guild.subject ) {
									flips.push( {
										key: "UIIT_MSG_GUILD_CONNECT_NOTICE",
										value: 0,
										nativeType: 0,
										arguments: [ guild.subject ]
									} );
								}
							} else {
								flips.push( {
									key: "UIIT_MSG_GUILD_CONNECT",
									value: 0,
									nativeType: 0,
									arguments: [ old.name ]
								} );
							}
						} else if ( !self ) {
							flips.push( {
								key: "UIIT_MSG_GUILD_DISCONNECT",
								value: 0,
								nativeType: 0,
								arguments: [ old.name ]
							} );
						}
					}
					guild = { ...guild, members: guild.members.map( m => m.id === id ? { ...m, ...patch } : m ) };
				}
				if ( flips.length ) next = { ...next, notices: flips };
			} else {
				throw Error( "Unsupported guild update" );
			}
			if ( next.guild ) {
				next = { ...next, guild };
			}
		}
	} else if ( op === 0xb332 && lead === 1 ) {
		// 75CBD0 -> 82CEE0: the candidates the vote row asked for (0x7332),
		// each with its ballots, as v1.188 writes them (5E79C0).
		u8();
		const id = u32(), remainingMs = u32(), count = u8(), guild = next.guild;
		const candidates = Array.from( { length: count }, () => {
			const candidate = u32();
			return { id: candidate, ballots: u8() };
		} );
		if ( guild?.votes?.some( row => row.id === id ) ) {
			next = {
				...next,
				guild: { ...guild, votes: withVoteCandidates( guild.votes, id, remainingMs, candidates ) }
			};
		}
	} else {
		const result = u8();
		if ( result === 2 ) {
			const code = u8();
			if ( op === 0xb663 && code === 0x3c ) {
				// 75FB65 reads seconds; 75FBC7 pushes minutes/hours/days.
				const seconds = u32(), minutes = Math.floor( seconds / 60 ), hours = Math.floor( minutes / 60 );
				next = {
					...next,
					error: null,
					unresolvedNotice: undefined,
					notice: {
						key: "UIIT_MSG_GUILD_SECESSION_PENALTY",
						value: 0,
						nativeType: 0,
						arguments: [ String( Math.floor( hours / 24 ) ), String( hours % 24 ), String( minutes % 60 ) ]
					}
				};
			} else if ( op === 0xb515 && code === 0x48 ) {
				// 7682F0: the warehouse is in a member's hands; it names them.
				next = {
					...next,
					error: null,
					unresolvedNotice: undefined,
					notice: { key: "UIIT_MSG_GUILD_WAREHOUSE_USE", value: 0, nativeType: 0, arguments: [ str() ] }
				};
			} else if ( op === 0xb6dc && code === 0x33 ) {
				// 760270 names this one itself, outside the category table.
				next = {
					...next,
					error: null,
					unresolvedNotice: undefined,
					notice: { key: "UIIT_MSG_MRELEASEERR_NOTVOTETIME", value: 0, nativeType: 0 }
				};
			} else {
				// 75CC50 reads the ballot's and the candidate request's refusals
				// in category 0x15.
				const resolution = resolveNativeNotice( op === 0xb330 || op === 0xb332 ? 0x15 : 16, code );
				next = {
					...next,
					error: null,
					notice: resolution.kind === "notice" ? resolution.notice : undefined,
					unresolvedNotice: resolution.kind === "context" ?
						{ category: resolution.category, code: resolution.code } :
						undefined
				};
			}
		} else if ( result !== 1 ) {
			throw Error( "Invalid guild result" );
		} else if ( op === 0xb322 ) {
			// 766D30: a nonzero attribute adds bits; zero clears the set.
			const flags = u8();
			if ( next.guild ) {
				next = {
					...next,
					soldierAttributeSequence: (next.soldierAttributeSequence ?? 0) + 1,
					guild: { ...next.guild, flags: flags ? (next.guild.flags ?? 0) | flags : 0 }
				};
			}
		} else if ( op === 0xb515 ) {
			// The warehouse owner (storage-room.ts) carries the open path.
		} else if ( op === 0xb140 ) {
			next = { ...next, compensation: u32() };
		} else if ( op === 0xb7d4 || op === 0xb6dc ) {
			next = {
				...next,
				notice: {
					key: op === 0xb7d4 ? "UIIT_MSG_MLEAVE_SUCCESS" : "UIIT_MSG_MRELEASE_VOTING",
					value: 0,
					nativeType: 0
				}
			};
		} else if ( op === 0xb3f7 ) {
			next = { ...next, compensation: undefined };
		} else if ( op === 0xb663 ) {
			// 75F9B0 @75fa7f: a created guild announces itself on the guide
			// (67a600 @75fab1) with its name.
			const created = guildBlock( lane );
			next = {
				...next,
				guild: created,
				notice: {
					key: "UIIT_MSG_GUILD_CREATE_RESULT",
					value: 0,
					nativeType: 0,
					arguments: [ created.name ]
				}
			};
		} else if ( op === 0xb77a ) {
			// 75C950 @75c96b: the notice-edit ack raises the centre banner
			// (tail-jmp 67d030), like the sub-op 5 broadcast.
			next = {
				...next,
				notice: {
					key: "UIIT_MSG_GUILD_COMMON_KNOW_REMIND_UPDATE",
					value: 0,
					notificationBanner: true,
					bannerOnly: true
				}
			};
		} else if ( op === 0xb56e || op === 0xb66e ) {
			next = {
				...next,
				guild: null,
				alliances: [],
				allianceMaster: 0,
				allianceCrests: undefined,
				wars: [],
				warCountdown: undefined,
				warResult: undefined
			};
		} else if ( op === 0xb40f ) {
			// 766B52: acknowledgement is a guide, not an SP/GP mutation.
			const amount = u32();
			next = {
				...next,
				notice: {
					key: "UIIT_MSG_GUILD_GP_SUBSCRIPION_RESULT",
					value: amount,
					arguments: [ String( amount ) ],
					nativeType: 0
				}
			};
		} else if ( op === 0xb2bc ) {
			u32();
			const id = u32();
			const value = { grant: str() };
			if ( next.guild ) {
				next = {
					...next,
					guild: {
						...next.guild,
						members: next.guild.members.map( m => m.id === id ? { ...m, ...value } : m )
					}
				};
			}
		} else if ( op === 0xb65f ) {
			u32();
			const id = u32();
			const role = u8();
			// 7689A0 raises a Confirm-titled box on this answer (6888C0,
			// title UIIT_STT_CONFIRM_BOX): the formatted body names the
			// member and, for a grant, the position title; the title's
			// length selects grant (non-empty) over remove (empty).
			// A refusal never reaches here; it routes through the
			// category-0x10 arm above. Two safe-side divergences: an
			// untitled non-zero role keeps the member's previous title
			// natively and prints the grant with it, where the port
			// stays silent (the server only ever writes single-bit
			// roles, so the byte is unsendable); and a member the
			// roster does not hold faults natively on the unchecked
			// record, where the port stays silent.
			const target = next.guild?.members.find( m => m.id === id );
			if ( next.guild ) {
				next = {
					...next,
					guild: {
						...next.guild,
						members: next.guild.members.map( m => m.id === id ? { ...m, role } : m )
					}
				};
			}
			const title = POSITION_TITLES[role];
			if ( target && role === 0 ) {
				next = {
					...next,
					notice: {
						key: "",
						value: 0,
						bannerOnly: true,
						dialog: { title: "UIIT_STT_CONFIRM_BOX", lines: [ "UIIT_MSG_FORT_POSITION_REMOVE_COMPLETE" ] },
						arguments: [ target.name ]
					}
				};
			} else if ( target && title ) {
				next = {
					...next,
					notice: {
						key: "",
						value: 0,
						bannerOnly: true,
						dialog: { title: "UIIT_STT_CONFIRM_BOX", lines: [ "UIIT_MSG_FORT_POSITION_GRANT_COMPLETE" ] },
						arguments: [ target.name, "" ],
						localizedArguments: [ null, title ]
					}
				};
			}
		}
	}
	return next;
}

/*
================
guildUpdateIgnored

7637DC folds these subOps onto the default block at 763764: the native
handler consumes the subOp byte and returns without reading a body.
================
*/
function guildUpdateIgnored( type: number ): boolean {
	return type === 0x04 || type === 0x13 || type === 0x15 || type === 0x1e ||
		(type >= 0x07 && type <= 0x0c) || (type >= 0x0f && type <= 0x11) ||
		(type >= 0x17 && type <= 0x18) || (type >= 0x20 && type <= 0x22) || (type >= 0x24 && type <= 0x31);
}

/*
================
guildMember
================
*/
function guildMember( lane: SocialGuildLane ): GuildMember {
	const u8 = lane.u8, u32 = lane.u32, str = lane.str;
	const id = u32(), name = str(), grade = u8(), level = u8(), donated = u32(), permissions = u32();
	const warScore = u32(), warKills = u32(), warDeaths = u32();
	const grant = str(), model = u32(), role = u8(), offline = u8();
	if ( !id || offline > 1 ) {
		throw Error( "Invalid guild member" );
	}
	return {
		id,
		name,
		grade,
		level,
		donated,
		permissions,
		grant,
		model,
		role,
		offline,
		warScore,
		warKills,
		warDeaths
	};
}

/*
================
guildBlock
================
*/
function guildBlock( lane: SocialGuildLane ): Guild {
	const u8 = lane.u8, u32 = lane.u32, str = lane.str;
	const id = u32(), name = str(), level = u8(), gp = u32(), subject = str(), contents = str();
	const crest = u32();
	u8();
	const n = u8();
	if ( n > 250 ) {
		throw Error( "Guild roster budget" );
	}
	const members = Array.from( { length: n }, () => guildMember( lane ) );
	assertUniqueRosterRows( members, 250 );
	const votes = Array.from( { length: u8() }, () => ({ id: u32(), kind: u8(), remainingMs: u32() }) );
	assertUniqueRosterRows( votes, 255 );
	return { id, name, level, gp, subject, contents, members, crest, ...(votes.length ? { votes } : {}) };
}

/*
================
readWar
================
*/
function readWar( lane: SocialGuildLane ): GuildWar | null {
	const u8 = lane.u8, u32 = lane.u32, str = lane.str;
	const state = lane.state, noticeContext = lane.noticeContext;
	const id = u32();
	if ( !id ) return null;
	const word3c = u32(),
		type = u8(),
		word38 = u32(),
		a = u32(),
		b = u32(),
		scoreA = u32(),
		scoreB = u32(),
		name = str();
	const local = state.guild?.id;
	if ( local !== a && local !== b ) return null;
	return {
		id,
		enemyId: local === a ? b : a,
		name,
		type,
		localScore: local === a ? scoreA : scoreB,
		enemyScore: local === a ? scoreB : scoreA,
		word38,
		word3c,
		clockAt: noticeContext.now ?? 0
	};
}

/*
================
mergeWar
================
*/
function mergeWar( rows: readonly GuildWar[], row: GuildWar | null ) {
	if ( !row ) return rows;
	const result = rows.filter( r => r.id !== row.id );
	result.push( row );
	assertUniqueRosterRows( result, 255 );
	return result;
}
