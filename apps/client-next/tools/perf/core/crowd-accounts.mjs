/*
===========================================================================

crowd-accounts.mjs - a large crowd held from saved Agent sessions

A crowd admitted by crowd.mjs logs every peer in on every run, and the
Agent paces logins per client address (login_limiter.go: burst 10, then
one per 6 s): every local peer shares 127.0.0.1, so 600 peers would wait
an hour each time. An Agent session lasts 12 hours
(auth.AgentSessionLifetime), so this owner pays those logins once:

  provisionAccounts  creates scratch accounts and characters into an
                     accounts file and saves each account's session.
                     It waits out the login budget instead of failing,
                     and a second run with the same file continues where
                     the first stopped without creating an account twice;
  holdCrowd          stands the first N characters on crowdLayout and
                     connects them on their saved sessions, logging in
                     again only where a session has expired;
  retireAccounts     disables every account of the file, once the crowd
                     is no longer wanted. Holding never disables them.

The file holds passwords and bearer tokens: it is written owner-only and
replaced atomically, and nothing from it enters a result artifact.

===========================================================================
*/
import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { readFile, rename, writeFile } from "node:fs/promises";
import { fetchProbeSessionJson } from "../../../../../scripts/lib/probeSession.mjs";
import { resetMissionMovementFixture } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { assertCharacterAllowed } from "../../../../../scripts/lib/probeCharacter.mjs";
import { parseActivity, startActivity } from "./crowd-activity.mjs";
import {
	connectPeer,
	createLoginGate,
	crowdLayout,
	DEFAULT_ACTIVITY_SEED,
	DEFAULT_RETRY_AFTER_S,
	openProvisioning
} from "./crowd.mjs";

const ACCOUNTS_VERSION = 1;
// Account "held<prefix><index>", character "H<prefix><index>": 10 bytes, inside
// the 12-byte name limit (domain.CharacterNameMaxBytes). The prefix is random
// per file, so two files never claim the same account.
const ACCOUNT_ID_STEM = "held", CHARACTER_STEM = "H", PREFIX_BYTES = 3, INDEX_DIGITS = 3;
const PASSWORD_BYTES = 24;
const HTTP_UNAUTHORIZED = 401, HTTP_CONFLICT = 409;
// Peers a hold stands and connects at once; logins still go one at a time.
const HOLD_CONCURRENCY = 16;
// A character a previous hold just released can still be in play for a
// while; the fixture reset waits for it (missionMovementFixture.mjs).
const RESET_TIMEOUT_MS = 60000;
const HOLD_PROGRESS_EVERY = 50;

/*
================
newAccountsFile

An empty accounts file for fixture, with its own random name prefix.
================
*/
export function newAccountsFile( fixture ) {
	return { version: ACCOUNTS_VERSION, fixture, prefix: randomBytes( PREFIX_BYTES ).toString( "hex" ), accounts: [] };
}

/*
================
loadAccountsFile
================
*/
export async function loadAccountsFile( path ) {
	const file = JSON.parse( await readFile( path, "utf8" ) );
	assert.ok( file?.version === ACCOUNTS_VERSION && Array.isArray( file.accounts ), `${path} is no accounts file` );
	return file;
}

/*
================
createAccountsSaver

save( file ) writes the whole file to a sibling and renames it over path,
so an interrupted write never leaves a torn file (its passwords are the
only record of the accounts). Writes are chained in call order.
================
*/
export function createAccountsSaver( path ) {
	let tail = Promise.resolve();
	return file => {
		const text = JSON.stringify( file, null, 1 ), scratch = `${path}.${process.pid}.tmp`;
		tail = tail.then( async () => {
			await writeFile( scratch, text, { mode: 0o600 } );
			await rename( scratch, path );
		} );
		return tail;
	};
}

/*
================
planAccounts

Extends file to count accounts. Existing records are kept exactly as they
are, so an interrupted pass resumes on the same ids and passwords; only
the missing indices get new ones.
================
*/
export function planAccounts( file, count ) {
	for ( let index = file.accounts.length; index < count; index++ ) {
		const tag = `${file.prefix}${String( index ).padStart( INDEX_DIGITS, "0" )}`;
		file.accounts.push( {
			id: `${ACCOUNT_ID_STEM}${tag}`,
			password: randomBytes( PASSWORD_BYTES ).toString( "hex" ),
			character: assertCharacterAllowed( `${CHARACTER_STEM}${tag}`, { context: "held crowd accounts" } ),
			provisioned: false,
			created: false,
			retired: false,
			session: null
		} );
	}
	return file;
}

/*
================
withSession

Runs use( session ) on the account's saved session, and once more on a
new login when there is none or the Agent answers 401 (expired). A new
session is saved before it is used. Returns whether it logged in.
================
*/
async function withSession( account, { login, save, file }, use ) {
	if ( account.session ) {
		try {
			await use( account.session );
			return false;
		} catch ( error ) {
			if ( error?.status !== HTTP_UNAUTHORIZED ) throw error;
		}
	}
	account.session = await login( account.id, account.password );
	await save( file );
	await use( account.session );
	return true;
}

/*
================
openAgent

The Agent calls provisioning makes, as one plain struct, so the pass can
be driven against a fake: createAccount, verifyAccount, disableAccount,
lookupAccount (provisioning API), login (the paced password login),
createCharacter and listCharacters (on a session).
================
*/
export async function openAgent( { provisioningUrl, tokenPath } ) {
	const request = await openProvisioning( provisioningUrl, tokenPath );
	const login = createLoginGate( Infinity );
	return {
		createAccount: ( id, password ) => request( "/v1/accounts", "POST", { id, password } ),
		verifyAccount: async ( id, password ) =>
			(await request( `/v1/accounts/${id}/verify`, "POST", { password } ))?.valid === true,
		disableAccount: id => request( `/v1/accounts/${id}/disabled`, "PUT", { disabled: true } ),
		lookupAccount: id => request( `/v1/accounts/${id}`, "GET" ),
		login,
		createCharacter: ( session, name ) =>
			fetchProbeSessionJson( session, "/character/create", {
				method: "POST",
				headers: { "content-type": "application/json" },
				body: JSON.stringify( {
					characterName: name,
					modelCodename: "CHAR_CH_MAN_ADVENTURER",
					heightIndex: 0,
					volumeIndex: 0,
					weaponIndex: 1,
					protectorIndex: 0,
					armorSelected: false,
					weaponSelected: true
				} )
			} ),
		listCharacters: async session =>
			((await fetchProbeSessionJson( session, "/character/list" ))?.characters ?? []).map( entry => entry?.name )
	};
}

/*
================
loginWait

How long that many logins take at the Agent's steady pace (progress lines).
================
*/
function loginWait( logins ) {
	const seconds = logins * DEFAULT_RETRY_AFTER_S;
	return seconds < 120 ? `${seconds} s` : `${Math.round( seconds / 60 )} min`;
}

/*
================
provisionAccounts

Brings the first count accounts of file to created, in order, saving
after every step. An account is POSTed only while its record says it is
not provisioned; a 409 then means an earlier run's POST landed before it
could save, and the account is taken over only when the saved password
verifies. A character create that fails is checked against the account's
list the same way. log( line ) gets progress with an estimate at the
Agent's steady login pace.
================
*/
export async function provisionAccounts( { file, count, agent, save, log = console.log } ) {
	crowdLayout( file.fixture, count );
	assert.ok( !file.accounts.some( account => account.retired ), "the accounts file is retired; start a new one" );
	planAccounts( file, count );
	await save( file );
	const pending = file.accounts.slice( 0, count ).filter( account => !account.created );
	const started = count - pending.length;
	log(
		`[crowd-accounts] ${started}/${count} ready; ${pending.length} to create, about ${
			loginWait( pending.length )
		} at the Agent's login pace`
	);
	const context = { login: agent.login, save, file };
	let logins = 0;
	for ( const [done, account] of pending.entries() ) {
		if ( !account.provisioned ) {
			try {
				await agent.createAccount( account.id, account.password );
			} catch ( error ) {
				if ( error?.status !== HTTP_CONFLICT ) throw error;
				assert.ok(
					await agent.verifyAccount( account.id, account.password ),
					`${account.id} exists with another password; it is not this file's`
				);
			}
			account.provisioned = true;
			await save( file );
		}
		const loggedIn = await withSession( account, context, async session => {
			try {
				await agent.createCharacter( session, account.character );
			} catch ( error ) {
				if ( error?.status === HTTP_UNAUTHORIZED ) throw error;
				if ( !(await agent.listCharacters( session )).includes( account.character ) ) throw error;
			}
		} );
		if ( loggedIn ) logins++;
		account.created = true;
		await save( file );
		const left = pending.length - done - 1;
		log(
			`[crowd-accounts] ${started + done + 1}/${count} ready${left ? `, about ${loginWait( left )} left` : ""}`
		);
	}
	return { created: pending.length, logins };
}

/*
================
retireAccounts

Disables every provisioned account of the file and reads each back from
the Agent. A retired file is kept, so nothing provisions over it.
================
*/
export async function retireAccounts( { file, agent, save } ) {
	let disabled = 0;
	for ( const account of file.accounts ) {
		if ( !account.provisioned || account.retired ) continue;
		await agent.disableAccount( account.id );
		assert.ok(
			(await agent.lookupAccount( account.id ))?.disabled === true,
			`${account.id} did not read back disabled`
		);
		account.retired = true;
		account.session = null;
		disabled++;
		await save( file );
	}
	return { disabled, accounts: file.accounts.length };
}

/*
================
holdCrowd

The first count characters of the file, in world: each stands on its
crowdLayout spot (a fixture reset on its session, so every hold starts the
same scene) and connects. A session the Agent no longer honours is
replaced by one paced login. Returns the crowd shape createCrowd returns
({ peers, activity, send, listen, close }) plus source: how many sessions
were reused and how many logins the hold spent. close() stops the
activity, closes the sockets and saves the file; the accounts stay.
================
*/
export async function holdCrowd( { file, count, save, act = "", seed = DEFAULT_ACTIVITY_SEED } ) {
	const activity = parseActivity( act );
	const spots = crowdLayout( file.fixture, count );
	const accounts = file.accounts.slice( 0, count );
	assert.ok(
		accounts.length === count && accounts.every( account => account.created && !account.retired ),
		`the accounts file holds ${
			file.accounts.filter( account => account.created && !account.retired ).length
		} usable characters, fewer than ${count}; provision first`
	);
	const context = { login: createLoginGate( Infinity ), save, file };
	const byIndex = new Map(), source = { kind: "accounts", accounts: count, reused: 0, logins: 0 };
	let running = null;
	/*
	================
	close
	================
	*/
	const close = async () => {
		running?.stop();
		for ( const peer of byIndex.values() ) peer.close();
		await save( file );
	};
	/*
	================
	hold
	================
	*/
	const hold = async index => {
		const account = accounts[index];
		const loggedIn = await withSession( account, context, session =>
			resetMissionMovementFixture( {
				session,
				characterName: account.character,
				timeoutMs: RESET_TIMEOUT_MS,
				fixture: { ...file.fixture, start: spots[index] }
			} ) );
		if ( loggedIn ) source.logins++;
		else source.reused++;
		byIndex.set( index, await connectPeer( account.session, account.character ) );
		if ( byIndex.size % HOLD_PROGRESS_EVERY === 0 || byIndex.size === count ) {
			console.log( `[crowd-accounts] in world ${byIndex.size}/${count} (${source.logins} logins)` );
		}
	};
	try {
		// After the first failure no worker starts another peer; the ones in
		// flight settle before close() so no socket opens behind it.
		let next = 0, failing = false;
		const worker = async () => {
			while ( !failing && next < count ) {
				await hold( next++ ).catch( error => {
					failing = true;
					throw error;
				} );
			}
		};
		const settled = await Promise.allSettled( Array.from( { length: HOLD_CONCURRENCY }, worker ) );
		const failed = settled.find( result => result.status === "rejected" );
		if ( failed ) throw failed.reason;
	} catch ( error ) {
		await close();
		throw error;
	}
	const crowd = {
		peers: accounts.map( ( _, index ) => byIndex.get( index ).evidence ),
		source,
		close,
		send: ( index, opcode, payload ) => byIndex.get( index )?.send( opcode, payload ),
		listen: ( index, listener ) => byIndex.get( index )?.listen( listener )
	};
	running = startActivity( { crowd, count, activity, seed, spotFor: index => spots[index] } );
	crowd.activity = running.evidence;
	return crowd;
}
