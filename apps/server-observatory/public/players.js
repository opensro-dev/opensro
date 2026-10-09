/*
===========================================================================
players.js - explicit inspection, evidence export, and audited recovery forms
===========================================================================
*/
/*
================
byID
================
*/
function byID( id ) {
	return document.getElementById( "recovery-" + id );
}
const status = byID( "status" );
let snapshot = null;
let selectedShard = "";
let selectedName = "";
let inspectedInput = "";
let enabled = false, mutating = false, inspecting = false, inspection = 0;
const MIN_REASON_LENGTH = 5, MAX_REASON_LENGTH = 500;
const townNames = {
	GATE_CH: "Jangan",
	GATE_KT: "Hotan",
	GATE_WC: "Donwhang",
	GATE_CA: "Samarkand",
	GATE_EU: "Constantinople"
};

/*
================
syncControls

One mutation owns the inspected identity until its outcome is known.
================
*/
function syncControls() {
	byID( "inspect-button" ).disabled = !enabled || mutating || inspecting;
	byID( "shard" ).disabled = mutating;
	byID( "character" ).disabled = mutating;
	for ( const form of [ "rescue", "clear-pk" ] ) {
		for ( const control of byID( form ).elements ) control.disabled = !enabled || mutating || !snapshot;
	}
	byID( "clear-pk-button" ).disabled ||= !snapshot?.player || !("pk" in snapshot.player);
}
/*
================
invalidateSelection

Changing either search field retires confirmations and in-flight inspections.
================
*/
function invalidateSelection() {
	inspection++;
	inspecting = false;
	snapshot = null;
	selectedName = selectedShard = "";
	inspectedInput = "";
	byID( "result" ).hidden = true;
	for ( const id of [ "confirmation", "pk-confirmation", "reason", "pk-reason" ] ) byID( id ).value = "";
	status.textContent = "Inspect this character and server before making changes.";
	syncControls();
}
/*
================
pkSummary
================
*/
function pkSummary( player ) {
	if ( !("pk" in player) ) return "Unavailable on this shard";
	const pk = player.pk;
	return `Penalty ${pk?.penalty ?? 0}; daily kills ${pk?.dailyCount ?? 0}; total kills ${pk?.totalCount ?? 0}`;
}

/*
================
requestJSON
================
*/
async function requestJSON( url, options = {} ) {
	const response = await fetch( url, { cache: "no-store", signal: AbortSignal.timeout( 20000 ), ...options } );
	const text = await response.text();
	let result;
	try {
		result = JSON.parse( text );
	} catch {
		throw Error( text || "Console returned an empty response" );
	}
	if ( !response.ok ) throw Error( result.error ?? "Console request failed" );
	return result;
}
/*
================
showPlayer
================
*/
function showPlayer( value, shard ) {
	if ( value.shard !== shard ) throw Error( "Character response belongs to another server" );
	snapshot = value;
	selectedShard = shard;
	selectedName = value.player.name;
	inspectedInput = byID( "character" ).value.trim();
	byID( "result" ).hidden = false;
	byID( "player-title" ).textContent = selectedName;
	const player = value.player, spawn = player.savedWorld?.spawn;
	byID( "session" ).textContent = player.bound ? "SESSION BOUND" : "OFFLINE";
	const facts = [
		[ "Server", byID( "shard" ).selectedOptions[0]?.textContent + " (" + shard + ")" ],
		[ "Character ID", player.id ],
		[ "PK record", pkSummary( player ) ],
		[ "PVP state", [ "Neutral", "Aggressor", "Murderer" ][player.pvpState] ?? "Unavailable" ],
		[
			"Active aggressions",
			"aggressions" in player ? Object.keys( player.aggressions ?? {} ).length : "Unavailable"
		],
		[ "Level", player.level ],
		[ "Health", player.hp ],
		[ "Mana", player.mp ],
		[ "Region", spawn?.regionId ?? "Unknown" ],
		[ "Position", spawn ? `${spawn.x.toFixed( 1 )}, ${spawn.y.toFixed( 1 )}, ${spawn.z.toFixed( 1 )}` : "Unknown" ],
		[ "Captured", new Date( value.capturedAt ).toLocaleTimeString() ]
	];
	byID( "facts" ).replaceChildren( ...facts.map( ( [label, value] ) => {
		const row = document.createElement( "div" ),
			term = document.createElement( "dt" ),
			detail = document.createElement( "dd" );
		term.textContent = label;
		detail.textContent = String( value );
		row.append( term, detail );
		return row;
	} ) );
	byID( "state" ).textContent = JSON.stringify( value, null, 2 );
	byID( "town" ).replaceChildren( ...value.towns.map( town => {
		const option = document.createElement( "option" );
		option.value = String( town.id );
		option.textContent = townNames[town.code] ?? town.code;
		return option;
	} ) );
	for ( const id of [ "confirmation", "pk-confirmation" ] ) byID( id ).value = "";
	syncControls();
}
/*
================
inspectPlayer
================
*/
async function inspectPlayer( event ) {
	event.preventDefault();
	if ( !enabled || mutating ) return;
	const shard = byID( "shard" ).value, name = byID( "character" ).value.trim();
	invalidateSelection();
	const current = inspection;
	inspecting = true;
	status.textContent = "Reading character authority...";
	syncControls();
	try {
		const result = await requestJSON( "/api/player?" + new URLSearchParams( { shard, character: name } ) );
		if ( current !== inspection ) return;
		showPlayer( result, shard );
		status.textContent = "Snapshot captured. Download it before making changes to preserve the original state.";
	} catch ( error ) {
		if ( current === inspection ) status.textContent = error.message;
	} finally {
		if ( current === inspection ) inspecting = false;
		syncControls();
	}
}
/*
================
mutatePlayer
================
*/
async function mutatePlayer( event, action ) {
	event.preventDefault();
	if ( !enabled || mutating || inspecting ) return;
	const clearPK = action === "clear-pk", prefix = clearPK ? "pk-" : "";
	if ( !snapshot || byID( "shard" ).value !== selectedShard || byID( "character" ).value.trim() !== inspectedInput ) {
		invalidateSelection();
		return;
	}
	if ( byID( prefix + "confirmation" ).value !== selectedName ) {
		status.textContent = "Enter the inspected character's exact name to confirm.";
		return;
	}
	const reason = byID( prefix + "reason" ).value.trim();
	if ( reason.length < MIN_REASON_LENGTH || reason.length > MAX_REASON_LENGTH ) {
		status.textContent = "Enter a reason between 5 and 500 characters.";
		return;
	}
	if ( clearPK && !("pk" in snapshot.player) ) {
		status.textContent = "PK state is unavailable. This shard must support PK inspection before clearing it.";
		return;
	}
	const originalPK = pkSummary( snapshot.player );
	const characterID = snapshot.player.id, current = inspection;
	mutating = true;
	syncControls();
	const character = selectedName, shard = selectedShard;
	status.textContent = clearPK ?
		"Closing this player's session and clearing active PK..." :
		"Closing this player's session and saving the rescue...";
	try {
		const result = await requestJSON( "/api/player?" + new URLSearchParams( { shard } ), {
			method: "POST",
			headers: { "Content-Type": "application/json", "X-SRO-Console": "1" },
			body: JSON.stringify( {
				id: crypto.randomUUID(),
				character,
				...(clearPK ? { action: "clear-pk" } : { town: Number( byID( "town" ).value ) }),
				reason
			} )
		} );
		if ( current !== inspection ) return;
		if ( result.player.id !== characterID || result.player.name !== character ) {
			throw Error( "Operation returned a different character" );
		}
		showPlayer( result, shard );
		status.textContent = clearPK ?
			`${character} active PK cleared on ${shard}. Before: ${originalPK}. After: ${
				pkSummary( result.player )
			}. Daily and total kill history are retained. Ask the player to log in again.` :
			`${character} was rescued on ${shard}. Ask the player to log in again.`;
	} catch ( error ) {
		invalidateSelection();
		status.textContent =
			`${error.message}. Inspect the player again before another operation; the outcome may be uncertain.`;
	} finally {
		mutating = false;
		syncControls();
	}
}
/*
================
downloadSnapshot
================
*/
function downloadSnapshot() {
	if ( !snapshot ) return;
	const blob = new Blob( [ JSON.stringify( snapshot, null, 2 ) ], { type: "application/json" } );
	const url = URL.createObjectURL( blob ), link = document.createElement( "a" );
	link.href = url;
	link.download = "player-diagnostic-" + new Date().toISOString().replaceAll( ":", "-" ) + ".json";
	link.click();
	setTimeout( () => URL.revokeObjectURL( url ), 1000 );
}
/*
================
init
================
*/
async function init() {
	try {
		const config = await requestJSON( "/api/console" );
		byID( "operator" ).textContent = "Operator: " + config.operator;
		byID( "shard" ).replaceChildren( ...config.shards.map( shard => {
			const option = document.createElement( "option" );
			option.value = shard.id;
			option.textContent = shard.name;
			return option;
		} ) );
		if ( !config.enabled ) {
			status.textContent = "Player operations are not configured on this console.";
		}
		enabled = config.enabled === true;
	} catch ( error ) {
		status.textContent = error.message;
	} finally {
		syncControls();
	}
}
byID( "inspect" ).addEventListener( "submit", inspectPlayer );
byID( "rescue" ).addEventListener( "submit", event => void mutatePlayer( event, "rescue" ) );
byID( "clear-pk" ).addEventListener( "submit", event => void mutatePlayer( event, "clear-pk" ) );
byID( "shard" ).addEventListener( "change", invalidateSelection );
byID( "character" ).addEventListener( "input", invalidateSelection );
byID( "download" ).addEventListener( "click", downloadSnapshot );
syncControls();
void init();
