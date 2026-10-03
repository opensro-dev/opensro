/*
===========================================================================
players.js - explicit inspection, evidence export, and audited rescue forms
===========================================================================
*/
/*
================
byID
================
*/
function byID( id ) {
	return document.getElementById( id );
}
const status = byID( "status" );
let snapshot = null;
let selectedShard = "";
let selectedName = "";
const townNames = {
	GATE_CH: "Jangan",
	GATE_KT: "Hotan",
	GATE_WC: "Donwhang",
	GATE_CA: "Samarkand",
	GATE_EU: "Constantinople"
};

/*
================
requestJSON
================
*/
async function requestJSON( url, options = {} ) {
	const response = await fetch( url, { cache: "no-store", ...options } );
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
	snapshot = value;
	selectedShard = shard;
	selectedName = value.player.name;
	byID( "result" ).hidden = false;
	byID( "player-title" ).textContent = selectedName;
	const player = value.player, spawn = player.savedWorld?.spawn;
	byID( "summary" ).textContent = `${
		player.bound ? "Session bound" : "Offline"
	} | Level ${player.level} | HP ${player.hp} | Region ${spawn?.regionId ?? "unknown"} | ${spawn?.x ?? "?"}, ${
		spawn?.z ?? "?"
	}`;
	byID( "state" ).textContent = JSON.stringify( value, null, 2 );
	byID( "town" ).replaceChildren( ...value.towns.map( town => {
		const option = document.createElement( "option" );
		option.value = String( town.id );
		option.textContent = townNames[town.code] ?? town.code;
		return option;
	} ) );
}
/*
================
inspectPlayer
================
*/
async function inspectPlayer( event ) {
	event.preventDefault();
	const shard = byID( "shard" ).value, name = byID( "character" ).value.trim();
	byID( "result" ).hidden = true;
	snapshot = null;
	status.textContent = "Reading character authority...";
	try {
		showPlayer( await requestJSON( "/api/player?" + new URLSearchParams( { shard, character: name } ) ), shard );
		status.textContent = "Snapshot captured. Download it before rescue to preserve the original state.";
	} catch ( error ) {
		status.textContent = error.message;
	}
}
/*
================
rescuePlayer
================
*/
async function rescuePlayer( event ) {
	event.preventDefault();
	if ( !snapshot || byID( "confirmation" ).value !== selectedName ) {
		status.textContent = "Enter the inspected character's exact name to confirm.";
		return;
	}
	const button = byID( "rescue-button" );
	button.disabled = true;
	const character = selectedName, shard = selectedShard;
	status.textContent = "Closing this player's session and saving the rescue...";
	try {
		const result = await requestJSON( "/api/player?" + new URLSearchParams( { shard } ), {
			method: "POST",
			headers: { "Content-Type": "application/json", "X-SRO-Console": "1" },
			body: JSON.stringify( {
				id: crypto.randomUUID(),
				character,
				town: Number( byID( "town" ).value ),
				reason: byID( "reason" ).value.trim()
			} )
		} );
		showPlayer( result, shard );
		byID( "confirmation" ).value = "";
		status.textContent = `${character} was rescued. Ask the player to log in again.`;
	} catch ( error ) {
		status.textContent = error.message;
	} finally {
		button.disabled = false;
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
		if ( !config.enabled ) status.textContent = "Player operations are not configured on this console.";
	} catch ( error ) {
		status.textContent = error.message;
	}
}
byID( "inspect" ).addEventListener( "submit", inspectPlayer );
byID( "rescue" ).addEventListener( "submit", rescuePlayer );
byID( "download" ).addEventListener( "click", downloadSnapshot );
void init();
