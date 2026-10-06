/*
===========================================================================

crowd.mjs - authenticated development peers for live crowd acceptance

Peers use the production codec and real GameWorld sockets. Only accounts
created by this invocation are changed; cleanup closes sockets and disables
those accounts. No credentials or admission tickets enter result artifacts.

===========================================================================
*/
import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import { randomBytes } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import { createCodec } from "../../../src/engine/runtime/simulation/worker/network/codec/codec.ts";
import {
	openProbeAgentSession,
	fetchProbeSessionJson,
	mintProbeTransportAdmissionToken,
	mintProbeEnterWorldToken
} from "../../../../../scripts/lib/probeSession.mjs";
import { resetMissionMovementFixture } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { assertCharacterAllowed } from "../../../../../scripts/lib/probeCharacter.mjs";

const MAX_PEERS = 32;
const ADMISSION_TIMEOUT_MS = 30000;
const LOGIN_SPACING_MS = 6500;

/*
================
connectPeer
================
*/
async function connectPeer( session, character ) {
	const codec = createCodec();
	const admission = await mintProbeTransportAdmissionToken( session );
	const enter = await mintProbeEnterWorldToken( session, character );
	const endpoint = new URL( session.transportUrl );
	endpoint.protocol = endpoint.protocol === "https:" ? "wss:" : "ws:";
	endpoint.pathname = endpoint.pathname.replace( /\/$/, "" ) + "/transport/ws";
	const socket = new WebSocket( endpoint );
	socket.binaryType = "arraybuffer";
	const evidence = { character, frames: 0, ready: false, closed: false };
	try {
		await new Promise( ( resolve, reject ) => {
			const timer = setTimeout( () => reject( Error( "Crowd peer admission timed out" ) ), ADMISSION_TIMEOUT_MS );
			const fail = message => {
				clearTimeout( timer );
				reject( Error( message ) );
			};
			socket.onopen = () => socket.send( codec.encode( codec.hello( admission ) ) );
			socket.onerror = () => fail( "Crowd peer transport failed" );
			socket.onclose = () => {
				evidence.closed = true;
				if ( !evidence.ready ) fail( "Crowd peer closed before admission" );
			};
			socket.onmessage = event => {
				try {
					const frame = codec.decode( new Uint8Array( event.data ) );
					evidence.frames++;
					if ( frame.opcode === 2 ) {
						codec.welcome( frame.payload );
						socket.send( codec.encode( codec.enterWorld( session.divisionId, character, enter ) ) );
					} else if ( frame.opcode === 3 ) {
						socket.send( codec.encode( { opcode: 4, payload: frame.payload } ) );
					} else if ( frame.opcode === 7 ) {
						assert.ok( frame.payload.length >= 9 && frame.payload[0] === 1, "Crowd EnterWorld accepted" );
						socket.send( codec.encode( { opcode: 0x3012, payload: new Uint8Array() } ) );
						evidence.ready = true;
						clearTimeout( timer );
						resolve();
					} else if ( frame.opcode === 5 ) {
						evidence.closed = true;
						fail( "Crowd peer received server goodbye" );
					}
				} catch ( error ) {
					fail( String( error ) );
				}
			};
		} );
		return { evidence, close: () => socket.close() };
	} catch ( error ) {
		socket.close();
		throw error;
	}
}

/*
================
createCrowd

Provisioning is explicitly loopback-only and requires the existing local
authority token. Scratch actors form a grid inside the observer's sector.
================
*/
export async function createCrowd( { count, fixture, provisioningUrl, tokenPath, journalPath } ) {
	assert.ok( Number.isInteger( count ) && count > 0 && count <= MAX_PEERS );
	const url = new URL( provisioningUrl );
	assert.ok( url.protocol === "http:" && [ "127.0.0.1", "localhost", "[::1]" ].includes( url.hostname ) );
	const token = (await readFile( tokenPath, "utf8" )).trim();
	const accounts = [], peers = [], characters = [];
	const prefix = randomBytes( 3 ).toString( "hex" );
	/*
	================
	provision
	================
	*/
	async function provision( path, method, body ) {
		const response = await fetch( new URL( path, url ), {
			method,
			headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
			body: JSON.stringify( body ),
			signal: AbortSignal.timeout( ADMISSION_TIMEOUT_MS )
		} );
		assert.ok( response.ok, `Crowd provisioning ${method} failed: HTTP ${response.status}` );
	}
	/*
	================
	close
	================
	*/
	async function close() {
		for ( const peer of peers ) peer.close();
		const results = await Promise.allSettled(
			accounts.map( id => provision( `/v1/accounts/${id}/disabled`, "PUT", { disabled: true } ) )
		);
		const failures = results.filter( result => result.status === "rejected" );
		await writeFile(
			journalPath,
			JSON.stringify(
				{
					accounts: accounts.map( ( id, index ) => ({
						id,
						character: characters[index],
						disabled: results[index].status === "fulfilled"
					}) )
				},
				null,
				2
			)
		);
		if ( failures.length ) {
			throw new AggregateError(
				failures.map( result => result.reason ),
				`Crowd cleanup failed for ${failures.length} accounts; see ${journalPath}`
			);
		}
	}
	try {
		for ( let index = 0; index < count; index++ ) {
			// Respect the Agent's six-second login refill on the shared loopback
			// address. Crowd setup must not bypass or disable rate limiting.
			await delay( LOGIN_SPACING_MS );
			const id = `perf${prefix}${index}`, password = randomBytes( 24 ).toString( "hex" );
			const character = assertCharacterAllowed( `P${prefix}${index}`, { context: "live crowd fixture" } );
			await provision( "/v1/accounts", "POST", { id, password } );
			accounts.push( id );
			characters.push( character );
			await writeFile(
				journalPath,
				JSON.stringify(
					{
						accounts: accounts.map( ( id, index ) => ({
							id,
							character: characters[index],
							disabled: false
						}) )
					},
					null,
					2
				)
			);
			const session = await openProbeAgentSession( { loginId: id, loginPassword: password } );
			await fetchProbeSessionJson( session, "/character/create", {
				method: "POST",
				headers: { "content-type": "application/json" },
				body: JSON.stringify( {
					characterName: character,
					modelCodename: "CHAR_CH_MAN_ADVENTURER",
					heightIndex: 0,
					volumeIndex: 0,
					weaponIndex: 1,
					protectorIndex: 0,
					armorSelected: false,
					weaponSelected: true
				} )
			} );
			await resetMissionMovementFixture( {
				session,
				characterName: character,
				timeoutMs: ADMISSION_TIMEOUT_MS,
				fixture: {
					...fixture,
					start: {
						...fixture.start,
						x: fixture.start.x + index % 4 * 15,
						z: fixture.start.z + 35 + Math.floor( index / 4 ) * 15
					}
				}
			} );
			peers.push( await connectPeer( session, character ) );
			console.log( `[crowd] admitted ${peers.length}/${count}` );
		}
		return { peers: peers.map( peer => peer.evidence ), close };
	} catch ( error ) {
		let failure = error instanceof Error ? error : new Error( String( error ) );
		try {
			await close();
		} catch ( cleanup ) {
			failure = new AggregateError( [ failure, cleanup ], "crowd setup and cleanup failed" );
		}
		// The caller never receives a crowd on partial admission. Retain its
		// attempted names so it can verify those sessions left the server too.
		throw Object.assign( failure, { crowdNames: [ ...characters ] } );
	}
}
