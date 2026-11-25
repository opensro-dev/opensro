/*
===========================================================================

quests.ts - journal, marker and gathering transaction ownership

Reliable receipts own state changes. Refusals have no quest ID, so only the
matching pending operation releases its transaction. Gathering delays remain
separate from persisted journal minute timers.

===========================================================================
*/
import {
	decodeQuest,
	admitQuest,
	mergeQuest,
	decodeQuestAcknowledgement,
	admitQuestMarker,
	decodeQuestMarker
} from "@/engine/foundation/gameplay/quest";
import type { QuestRecord, QuestMarker, QuestProgressEvent, QuestGathering } from "@/engine/contracts/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
import {
	QUEST_ABORT_RESPONSE_OPCODE,
	QUEST_ACCEPT_RESPONSE_OPCODE,
	QUEST_GATHER_CANCEL_OPCODE,
	QUEST_GATHER_CANCEL_REPLY_OPCODE,
	QUEST_GATHER_START_OPCODE,
	QUEST_MARKER_OPCODE,
	QUEST_MARKER_REMOVE_OPCODE,
	QUEST_UPDATE_OPCODE
} from "@/engine/contracts/opcodes";
import { viewOf } from "@/engine/foundation/binary/byte-view";

// The active quests one character's state holds; a login or an add past it
// is refused whole. Exported for the tests that walk the published catalog.
export const MAX_QUESTS = 256;
const MAX_COMPLETED = 65535;
const MAX_MARKERS = 255;
const MAX_PROGRESS = 100;
const UINT32_MAX = 0xffffffff;
const SECOND_MS = 1000;

/*
================
createQuests

Sequence numbers survive bootstrap because UI can retain a mission through
teleportation. Transient gathering never resumes from an offline snapshot.
================
*/
export function createQuests( send: ( frame: WireFrame ) => void ) {
	let markers: readonly QuestMarker[] = [];
	let records: readonly QuestRecord[] = [];
	let completed: readonly number[] = [];
	let progress: readonly QuestProgressEvent[] = [];
	let pending = 0, pendingAck = 0, sequence = 0;
	let pendingReward = false;
	let refusal: { readonly refId: number; readonly reason: number; } | undefined;
	let gathering: QuestGathering | undefined;

	/*
	================
	clear

	Retire all world-owned records while retaining event sequence identity.
	================
	*/
	function clear() {
		markers = [];
		records = [];
		completed = [];
		progress = [];
		pending = 0;
		pendingAck = 0;
		pendingReward = false;
		refusal = undefined;
		gathering = undefined;
	}

	return {
		/*
		================
		bootstrap

		Validate the complete replacement before publishing any of its lanes.
		================
		*/
		bootstrap( value: unknown ) {
			const b = value as {
				character?: { activeQuests?: unknown[]; completedQuestIds?: unknown; trackedQuests?: unknown[]; };
			};
			const rows = b.character?.activeQuests ?? [];
			if ( !Array.isArray( rows ) || rows.length > MAX_QUESTS ) throw Error( "Quest residency budget" );
			const next = rows.map( admitQuest );
			if ( new Set( next.map( row => row.refId ) ).size !== next.length ) throw Error( "Duplicate quest" );
			const done = b.character?.completedQuestIds ?? [];
			if (
				!Array.isArray( done ) || done.length > MAX_COMPLETED ||
				done.some( id => !Number.isSafeInteger( id ) || id <= 0 || id > UINT32_MAX ) ||
				new Set( done ).size !== done.length
			) throw Error( "Invalid completed quest IDs" );
			const marks = b.character?.trackedQuests ?? [];
			if ( !Array.isArray( marks ) || marks.length > MAX_MARKERS ) throw Error( "Quest marker residency budget" );
			const nextMarkers = marks.map( admitQuestMarker );
			if ( new Set( nextMarkers.map( row => row.refId ) ).size !== nextMarkers.length ) {
				throw Error( "Duplicate quest marker" );
			}
			clear();
			markers = nextMarkers;
			completed = [ ...done ];
			records = next.map( row => mergeQuest( undefined, row ) );
		},
		/*
		================
		request

		Progress packets cannot release a pending journal operation.
		================
		*/
		request( refId: number, reward: boolean, picks?: readonly number[] ) {
			if ( pending ) throw Error( "Quest transaction pending" );
			const row = records.find( record => record.refId === refId );
			if ( !row || reward && row.u10 !== 2 ) throw Error( "Quest action unavailable" );
			if ( picks !== undefined && !reward ) throw Error( "Quest picks need a reward claim" );
			// A selection claim appends its pick: [u8 count][count x u32
			// itemRefId], the body the native reward handler reads.
			const payload = new Uint8Array( picks?.length ? 5 + 4 * picks.length : 4 );
			const view = new DataView( payload.buffer );
			view.setUint32( 0, refId, true );
			if ( picks?.length ) {
				if ( picks.length > 255 ) throw Error( "Quest pick budget" );
				view.setUint8( 4, picks.length );
				picks.forEach( ( id, i ) => view.setUint32( 5 + 4 * i, id, true ) );
			}
			send( { opcode: reward ? 0x729a : 0x71eb, payload } );
			pending = refId;
			pendingAck = reward ? 0xb29a : 0xb1eb;
			pendingReward = reward;
			refusal = undefined;
		},
		/*
		================
		cancelGathering

		Retain the row until B75D confirms the requested cancellation.
		================
		*/
		cancelGathering() {
			if ( !gathering ) return;
			const payload = new Uint8Array( 4 );
			new DataView( payload.buffer ).setUint32( 0, gathering.refId, true );
			send( { opcode: QUEST_GATHER_CANCEL_OPCODE, payload } );
		},
		/*
		================
		clearGathering

		World teardown retires the transient delay without discarding the journal.
		================
		*/
		clearGathering() {
			gathering = undefined;
		},
		/*
		================
		step

		6B1740 retires kind 1 at elapsed duration. The display clock never awards
		the item; a later server inventory result remains independent.
		================
		*/
		step( now: number ) {
			if ( !gathering || gathering.durationMs === 0 || now < gathering.startedAtMs + gathering.durationMs ) {
				return false;
			}
			gathering = undefined;
			return true;
		},
		/*
		================
		receive

		Decode before mutation. Native 766900 starts the collection gauge;
		766950 cancels a matching identity independently of journal state.
		================
		*/
		receive( frame: WireFrame, now = 0 ) {
			const p = frame.payload;
			if ( frame.opcode === QUEST_GATHER_START_OPCODE ) {
				if ( p.length !== 5 ) throw Error( "Invalid gathering start" );
				const refId = viewOf( p ).getUint32( 0, true );
				if ( !refId || !Number.isFinite( now ) ) throw Error( "Invalid gathering identity or clock" );
				gathering = { refId, startedAtMs: now, durationMs: p[4]! * SECOND_MS };
				return true;
			}
			if ( frame.opcode === QUEST_GATHER_CANCEL_REPLY_OPCODE ) {
				if ( p[0] === 2 && p.length === 2 ) return true;
				if ( p[0] !== 1 || p.length !== 5 ) throw Error( "Invalid gathering cancellation" );
				const refId = viewOf( p ).getUint32( 1, true );
				if ( gathering?.refId === refId ) gathering = undefined;
				return true;
			}
			if ( frame.opcode === QUEST_MARKER_OPCODE ) {
				const row = decodeQuestMarker( p );
				if ( markers.length >= MAX_MARKERS && !markers.some( marker => marker.refId === row.refId ) ) {
					throw Error( "Quest marker residency budget" );
				}
				markers = [ ...markers.filter( marker => marker.refId !== row.refId ), row ];
				return true;
			}
			if ( frame.opcode === QUEST_MARKER_REMOVE_OPCODE ) {
				if ( p.length !== 4 ) throw Error( "Invalid quest marker removal" );
				const id = new DataView( p.buffer, p.byteOffset, 4 ).getUint32( 0, true );
				if ( !id ) throw Error( "Invalid quest marker reference" );
				markers = markers.filter( marker => marker.refId !== id );
				return true;
			}
			if ( frame.opcode === QUEST_ACCEPT_RESPONSE_OPCODE || frame.opcode === QUEST_ABORT_RESPONSE_OPCODE ) {
				const result = decodeQuestAcknowledgement( p );
				if ( result === 2 && pending && pendingAck === frame.opcode ) {
					// A refused reward claim keeps its reason for the HUD's
					// error line; a refused give-up has none.
					if ( pendingReward ) refusal = { refId: pending, reason: p[1] ?? 0 };
					pending = 0;
					pendingAck = 0;
					pendingReward = false;
				}
				return true;
			}
			if ( frame.opcode !== QUEST_UPDATE_OPCODE ) return false;
			const delta = decodeQuest( p ), previous = records.find( row => row.refId === delta.refId );
			if ( delta.op === 2 && !previous ) throw Error( "Quest update without insertion" );
			if ( delta.op === 1 && previous ) throw Error( "Duplicate quest insertion" );
			if ( delta.op === 1 && records.length >= MAX_QUESTS ) throw Error( "Quest residency budget" );
			const next = delta.record ? mergeQuest( previous, delta.record ) : undefined;
			if ( next && previous ) {
				const changes = previous.contents.map( before => ({
					sequence: ++sequence,
					refId: delta.refId,
					before,
					after: next.contents.find( contents => contents.tag === before.tag )!
				}) );
				progress = [ ...progress, ...changes ].slice( -MAX_PROGRESS );
			}
			if ( delta.op >= 3 && !completed.includes( delta.refId ) ) completed = [ ...completed, delta.refId ];
			records = next ?
				(previous ? records.map( row => row.refId === delta.refId ? next : row ) : [ ...records, next ]) :
				records.filter( row => row.refId !== delta.refId );
			if ( pending === delta.refId && delta.op >= 3 ) {
				pending = 0;
				pendingAck = 0;
				pendingReward = false;
			}
			if ( gathering?.refId === delta.refId && delta.op >= 3 ) gathering = undefined;
			return true;
		},
		/*
		================
		state

		Publish immutable records; presentation cannot award a gathered item.
		================
		*/
		state() {
			return {
				questMarkers: markers,
				completedQuests: completed,
				quests: records,
				questPending: pending,
				questRefusal: refusal,
				questProgress: progress,
				questGathering: gathering
			};
		},
		clear
	};
}
