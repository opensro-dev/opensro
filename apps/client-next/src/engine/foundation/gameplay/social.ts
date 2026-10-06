/*
===========================================================================

social.ts - party, guild and invitation state and wire

Owns the decoded party roster (the shared 75DB30 member record), guild
updates and pending invitations. Outgoing commands live in social-request. A frame
is decoded completely before any state is published.

===========================================================================
*/
import { GUILD_WAR_PROPOSAL, guildWarProposalReply } from "./guild-war";
import type { WireFrame } from "@/engine/contracts/network";
import { resolveNativeNotice, type NativeNoticeContext } from "./native-notice";
import type { SocialState, PartyMember, GuildMember, GuildVote, Guild, GuildWar } from "./social-roster";
export type { SocialState, PartyMember, GuildMember, GuildVote, Guild, GuildWar } from "./social-roster";
// The 0x3393 type a resurrection skill proposes to a dead player.
export const RESURRECTION_PROPOSAL = 4;
// The 0x3393 type a revival with an rmut skill proposes (7644E0 case 7).
export const MUTATION_PROPOSAL = 8;
// 7644E0 case 6: a guild master proposes a union (confirm box 0x1D).
export const UNION_PROPOSAL = 6;
export { socialRequest, type SocialCommand } from "./social-request";

/*
================
emptySocial
================
*/
export function emptySocial( localName = "" ): SocialState {
	return { localName, self: 0, leader: 0, options: 0, members: [], guild: null, invitation: null, error: null };
}

/*
================
withoutResurrection

The state with the resurrection question closed. The slot is optional and
dropped rather than nulled, so a state that never held a question keeps
the exact shape it had before the slot existed.
================
*/
export function withoutResurrection( state: SocialState ): SocialState {
	if ( !state.resurrection ) {
		return state;
	}
	const { resurrection: _closed, ...rest } = state;
	return rest;
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
socialPacket

Decode a complete frame before publishing any state. Only evidenced
server-emitted arms enter this projection.
================
*/
export function socialPacket(
	state: SocialState,
	frame: WireFrame,
	noticeContext: NativeNoticeContext & { readonly now?: number; } = {}
): SocialState | null {
	const op = frame.opcode, p = frame.payload;
	if (
		![
			0x3393,
			0xb71b,
			0xb465,
			0xb0d5,
			0xb452,
			0xb51a,
			0xb095,
			0xb34a,
			0xb2db,
			0x35d6,
			0x3e58,
			0x32c4,
			0xb663,
			0x3b29,
			0xb56e,
			0xb66e,
			0xb77a,
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
			0x341e,
			0x32bb,
			0x34f3,
			0x37d4,
			0xb3f0,
			0xb322,
			0xb7d4,
			0xb140,
			0xb3f7,
			0xb6dc,
			0xb330,
			0xb515,
			0x3a6c
		].includes( op )
	) {
		return null;
	}
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	/*
	================
	take
	================
	*/
	function take( n: number ) {
		if ( o + n > p.length ) {
			throw Error( "Truncated social frame" );
		}
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		i16 = () => v.getInt16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true );
	/*
	================
	str
	================
	*/
	function str() {
		const n = u16();
		if ( n > 2048 ) {
			throw Error( "Social string budget" );
		}
		const at = take( n );
		return new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + n ) );
	}
	/*
	================
	partyMember
	================
	*/
	function partyMember( old?: PartyMember ): PartyMember {
		const decoded = readPartyMember( p, o, old );
		o = decoded.end;
		return decoded.member;
	}
	/*
	================
	guildMember
	================
	*/
	function guildMember(): GuildMember {
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
	unique
	================
	*/
	function unique<
		T extends {
			id: number;
		}
	>( rows: readonly T[], max: number ) {
		if ( rows.length > max || new Set( rows.map( r => r.id ) ).size !== rows.length ) {
			throw Error( "Invalid social roster" );
		}
	}
	/*
	================
	guildBlock
	================
	*/
	function guildBlock(): Guild {
		const id = u32(), name = str(), level = u8(), gp = u32(), subject = str(), contents = str();
		const crest = u32();
		u8();
		const n = u8();
		if ( n > 250 ) {
			throw Error( "Guild roster budget" );
		}
		const members = Array.from( { length: n }, guildMember );
		unique( members, 250 );
		const votes = Array.from( { length: u8() }, () => ({ id: u32(), kind: u8(), remainingMs: u32() }) );
		unique( votes, 255 );
		return { id, name, level, gp, subject, contents, members, crest, ...(votes.length ? { votes } : {}) };
	}
	/*
	================
	readWar
	================
	*/
	function readWar(): GuildWar | null {
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
		unique( result, 255 );
		return result;
	}
	let next: SocialState = { ...state, error: null, notice: undefined };
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
		for ( let i = 0; i < count; i++ ) wars = mergeWar( wars, readWar() );
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
		unique( alliances, 255 );
		next = { ...next, alliances, allianceMaster, allianceCrests };
	} else if ( op === 0x3393 ) {
		const type = u8();
		if (
			type !== 1 && type !== 2 && type !== 3 && type !== RESURRECTION_PROPOSAL && type !== 5 &&
			type !== UNION_PROPOSAL && type !== MUTATION_PROPOSAL && type !== GUILD_WAR_PROPOSAL
		) {
			return null;
		}
		const gid = u32();
		const war = type === GUILD_WAR_PROPOSAL ?
			{ name: str(), mode: u8(), period: u32(), scoreIndex: u8(), stake: u32() } :
			undefined;
		if ( !gid ) {
			throw Error( "Invalid invitation" );
		}
		// 7644E0 types 4 and 8 carry only the caster: {u8 type, u32 casterGid}.
		// They fill their own slot and leave a pending invitation untouched.
		next = type === RESURRECTION_PROPOSAL || type === MUTATION_PROPOSAL ?
			{ ...next, resurrection: { gid, ...(type === MUTATION_PROPOSAL ? { mutation: true } : {}) } } :
			{
				...next,
				...(war ? { warPending: 2 as const } : {}),
				invitation: {
					type,
					gid,
					...(war ? { war } : {}),
					...(type === 2 || type === 3 ? { options: u8() } : {})
				}
			};
	} // 75B450 / 75B4B0 / 75B520 use category ONE, unlike invite failures.
	// Membership remains authoritative on 3E58, not these acknowledgements.
	else if ( op === 0xb71b || op === 0xb465 ) {
		if ( op === 0xb71b ) next = { ...next, warPending: 0 };
		if ( u8() === 2 ) {
			const resolution = resolveNativeNotice( 0x10, u8(), noticeContext );
			next = { ...next, notice: resolution.kind === "notice" ? resolution.notice : undefined };
		}
	} else if ( op === 0xb095 || op === 0xb34a || op === 0xb2db ) {
		if ( u8() === 2 ) {
			const resolution = resolveNativeNotice( 1, u8(), noticeContext );
			next = {
				...next,
				notice: resolution.kind === "notice" ? resolution.notice : undefined,
				unresolvedNotice: resolution.kind === "context" ?
					{ category: resolution.category, code: resolution.code } :
					undefined
			};
		}
	} // 75B100: success has no body or membership authority. Failure uses
	// category 2. Other one-byte result flags are native no-ops.
	else if ( op === 0xb51a ) {
		const result = u8();
		if ( result === 2 ) next = { ...next, error: null, notice: partyNotice( u8() ) ?? undefined };
	} else if ( op === 0xb0d5 || op === 0xb452 ) {
		const result = u8();
		if ( result === 1 ) {
			next = { ...next, self: u32() };
		} else if ( result === 2 ) {
			const code = u8();
			// 75B170 calls 5C8020(6) on timeout: retire the existing party
			// prompt, NOT create a dialog or emit the category-2 guide.
			next = op === 0xb452 && code === 0x10 ?
				{
					...next,
					error: null,
					invitation: next.invitation?.type === 2 || next.invitation?.type === 3 ? null : next.invitation
				} :
				{ ...next, error: null, notice: partyNotice( code ) ?? undefined };
		} else {
			throw Error( "Invalid party result" );
		}
	} else if ( op === 0x35d6 ) {
		const flags = u8();
		if ( flags & ~3 ) {
			throw Error( "Invalid party flags" );
		}
		if ( flags & 1 ) {
			next = { ...next, leader: u32(), options: u8(), members: [] };
		}
		if ( next.options & ~7 ) {
			throw Error( "Invalid party options" );
		}
		if ( flags & 2 ) {
			const n = u8();
			if ( n > 8 ) {
				throw Error( "Party roster budget" );
			}
			const members = Array.from( { length: n }, () => partyMember() );
			unique( members, 8 );
			next = { ...next, members };
		}
	} else if ( op === 0x3e58 ) {
		const type = u8();
		if ( type === 1 ) {
			u8();
			next = { ...next, self: 0, leader: 0, options: 0, members: [] };
		} else if ( type === 2 ) {
			const member = partyMember();
			next = { ...next, members: [ ...next.members, member ] };
			unique( next.members, 8 );
		} else if ( type === 3 ) {
			const id = u32();
			u8();
			next = id === next.self ?
				{ ...next, self: 0, leader: 0, options: 0, members: [] } :
				{ ...next, members: next.members.filter( m => m.id !== id ) };
		} else if ( type === 9 ) {
			next = { ...next, leader: u32() };
		} else if ( type === 6 ) {
			const id = u32(), old = next.members.find( m => m.id === id );
			if ( !old ) {
				throw Error( "Unknown party delta member" );
			}
			const member = partyMember( old );
			if ( member.id !== id ) {
				throw Error( "Party delta identity changed" );
			}
			next = { ...next, members: next.members.map( m => m.id === id ? member : m ) };
		} else {
			throw Error( "Unsupported party update" );
		}
	} else if ( op === 0x32c4 ) {
		next = { ...next, guild: guildBlock() };
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
			// The window keeps no candidate list in v1.150, so a ballot only
			// moves counts the client cannot show.
			u8();
			u8();
			u8();
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
		if ( type === 0 || guildUpdateIgnored( type ) ) o = p.length;
		else if ( type === 0x32 ) {
			// 76309C: 0 refuses the proposal, 2 times a pending one out, and 3
			// opens the two-line native suggestion dialog (7632F5).
			const result = u8();
			next = guildWarProposalReply( next, result, result === 0 || result === 2 ? str() : "" );
			// Every other reply, including the suggestion dialog, reads no body.
		} else if ( type === 1 ) {
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
		} else {
			if ( !next.guild ) {
				throw Error( "Guild update without baseline" );
			}
			let guild = next.guild;
			if ( type === 0x0d ) {
				const row = { id: u32(), name: str(), level: u8(), master: str(), model: u32(), flags: u8() };
				const alliances = [ ...(next.alliances ?? []).filter( a => a.id !== row.id ), row ];
				unique( alliances, 255 );
				next = { ...next, alliances };
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
				next = {
					...next,
					crestUpdates,
					alliances: clear ? [] : next.alliances?.filter( a => a.id !== id ),
					...(clear ? { allianceCrests: undefined, allianceMaster: 0 } : {})
				};
			} else if ( type === 0x19 ) {
				const row = readWar();
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
				if ( !str() ) str();
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
					)
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
				const row = guildMember();
				const members = [ ...guild.members, row ];
				unique( members, 250 );
				guild = { ...guild, members };
			} else if ( type === 3 ) {
				const id = u32();
				u8();
				if ( guild.members.find( m => m.id === id )?.name === next.localName ) {
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
				for ( let i = 0; i < count; i++ ) {
					const id = u32(), mask = commonMask ?? u8(), old = guild.members.find( m => m.id === id );
					if ( !old || mask & ~0x7f ) throw Error( "Unsupported guild member delta" );
					const patch = {
						...(mask & 1 ? { offline: u8() } : {}),
						...(mask & 2 ? { level: u8() } : {}),
						...(mask & 4 ? { grade: u8() } : {}),
						...(mask & 8 ? { donated: u32() } : {}),
						...(mask & 16 ? { permissions: u32() } : {}),
						...(mask & 32 ? { name: str() } : {}),
						...(mask & 64 ? { role: u8() } : {})
					};
					guild = { ...guild, members: guild.members.map( m => m.id === id ? { ...m, ...patch } : m ) };
				}
			} else {
				throw Error( "Unsupported guild update" );
			}
			if ( next.guild ) {
				next = { ...next, guild };
			}
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
				// 75CC50 reads the ballot's refusal in category 0x15.
				const resolution = resolveNativeNotice( op === 0xb330 ? 0x15 : 16, code );
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
			next = { ...next, guild: guildBlock() };
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
		} else if ( op === 0xb2bc || op === 0xb65f ) {
			u32();
			const id = u32();
			const value = op === 0xb2bc ? { grant: str() } : { role: u8() };
			if ( next.guild ) {
				next = {
					...next,
					guild: {
						...next.guild,
						members: next.guild.members.map( m => m.id === id ? { ...m, ...value } : m )
					}
				};
			}
		}
	}
	if ( o !== p.length ) {
		throw Error( "Trailing social bytes" );
	}
	return next;
}

/*
================
readPartyMember

Shared 75DB30 member record: roster and matching approval use the same wire owner.
================
*/
export function readPartyMember( p: Uint8Array, start = 0, old?: PartyMember ): { member: PartyMember; end: number; } {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let at = start;
	/*
	================
	take
	================
	*/
	function take( n: number ) {
		if ( at + n > p.length ) throw Error( "Truncated party member" );
		const out = at;
		at += n;
		return out;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true ),
		i16 = () => v.getInt16( take( 2 ), true );
	const mask = u8();
	const member: {
		id: number;
		name: string;
		model: number;
		level: number;
		status: number;
		region: number;
		x: number;
		y: number;
		z: number;
		war: number;
		guild?: string;
		native41?: number;
		primaryMastery?: number;
		secondaryMastery?: number;
	} = { id: 0, name: "", model: 0, level: 0, status: 0, region: 0, x: 0, y: 0, z: 0, war: 0, ...old };
	if ( mask & 16 ) member.id = u32();
	if ( mask & 1 ) {
		const n = u16();
		if ( n > 2048 ) throw Error( "Social string budget" );
		const offset = take( n );
		member.name = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( offset, offset + n ) );
		member.model = u32();
	}
	if ( mask & 2 ) member.level = u8();
	if ( mask & 4 ) member.status = u8();
	if ( mask & 32 ) {
		member.region = u16();
		member.x = i16();
		member.y = i16();
		member.z = i16();
		member.war = u32();
	}
	if ( mask & 64 ) {
		const n = u16();
		if ( n > 2048 ) throw Error( "Social string budget" );
		const offset = take( n );
		member.guild = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( offset, offset + n ) );
	}
	if ( mask & 128 ) member.native41 = u8();
	if ( mask & 8 ) {
		member.primaryMastery = u32();
		member.secondaryMastery = u32();
	}
	if ( !member.id ) throw Error( "Missing party member identity" );
	return { member, end: at };
}
import { partyNotice } from "./party-notices";
