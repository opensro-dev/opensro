/*
===========================================================================
invites.js - the Invite keys page: make, find and revoke the website's keys

Keys are shown in full only in the answer that made them (the website
stores hashes), so the made-keys panel is the one chance to copy them. A
key for a Discord member is a grant the member collects from the bot, so
it shows no code at all (invite-made.js).
Revoking takes two clicks on the same button. Nothing here sees the
console's secret: the page talks to /api/invites on this console.
===========================================================================
*/
import { madeStatus } from "./invite-made.js";
import { escape } from "./model.js";
import { postJSON, requestJSON } from "./request.js";

const STATUS_TAG = { active: "ACTIVE", used: "USED", expired: "EXPIRED", revoked: "REVOKED" };
// A second click on Revoke within this window confirms it.
const CONFIRM_MS = 4000;
const NONE = '<span class="muted">—</span>';

/*
================
byID
================
*/
function byID( id ) {
	return document.getElementById( "invites-" + id );
}

/*
================
day
================
*/
function day( iso ) {
	return iso ? iso.slice( 0, 10 ) : "never";
}

/*
================
setStatus
================
*/
function setStatus( text ) {
	byID( "status" ).textContent = text;
}

/*
================
rowHTML
================
*/
function rowHTML( key ) {
	const revoke = key.status === "active" ?
		`<button class="button invites-revoke" type="button" data-id="${escape( key.id )}">Revoke</button>` :
		"";
	return `<tr data-state="${escape( key.status )}">
		<td class="invites-hint">…${escape( key.hint )}</td>
		<td>${escape( key.note ) || NONE}</td>
		<td><span class="tag invites-tag" data-state="${escape( key.status )}">${
		STATUS_TAG[key.status] ?? escape( key.status )
	}</span></td>
		<td>${escape( key.uses )}/${escape( key.maxUses )}</td>
		<td>${escape( key.redeemedBy.join( ", " ) ) || NONE}</td>
		<td>${escape( day( key.createdAt ) )}<br><span class="muted">${escape( key.createdBy )}</span></td>
		<td>${escape( day( key.expiresAt ) )}</td>
		<td>${revoke}</td>
	</tr>`;
}

/*
================
load

The key list, all keys or one Discord member's.
================
*/
async function load() {
	const member = byID( "filter-id" ).value.trim();
	const query = member ? "?" + new URLSearchParams( { discordUserId: member } ) : "";
	try {
		const { invites } = await requestJSON( "/api/invites" + query );
		byID( "rows" ).innerHTML = invites.length ?
			invites.map( rowHTML ).join( "" ) :
			`<tr><td colspan="8" class="muted">${member ? "This member has no keys." : "No keys yet."}</td></tr>`;
		const open = invites.filter( key => key.status === "active" ).length;
		setStatus( member ? `Discord member ${member}: ${invites.length} keys, ${open} open.` : `${open} open keys.` );
	} catch ( error ) {
		setStatus( error.message );
	}
}

/*
================
showMade

The keys just made, with copy buttons, and for a member every key they have.
================
*/
function showMade( result, member ) {
	byID( "made" ).hidden = false;
	// A grant has no code to copy; the bot hands it to the member.
	byID( "made-list" ).innerHTML = result.granted ?
		`<li>${escape( madeStatus( result ) )}</li>` :
		result.keys.map( key =>
			`<li>
		<code>${escape( key.code )}</code>
		<button class="button" type="button" data-copy="${escape( key.code )}">Copy key</button>
		<button class="button" type="button" data-copy="${escape( key.signupUrl )}">Copy signup link</button>
	</li>`
		).join( "" );
	const linked = byID( "linked" );
	linked.hidden = !result.linked;
	if ( !result.linked ) return;
	const lines = result.linked.map( key =>
		`<li><span class="invites-hint">…${escape( key.hint )}</span> · ${escape( key.status )} · ${
			escape( key.createdBy )
		}${key.redeemedBy.length ? " · redeemed by " + escape( key.redeemedBy.join( ", " ) ) : ""}</li>`
	);
	const count = result.linked.length;
	linked.innerHTML = `<h3>Discord member ${escape( member )} now has ${count} ${
		count === 1 ? "key" : "keys"
	}</h3><ul>${lines.join( "" )}</ul>`;
}

byID( "create" ).addEventListener( "submit", async event => {
	event.preventDefault();
	const member = byID( "discord-id" ).value.trim();
	const button = byID( "create-button" );
	button.disabled = true;
	setStatus( "Making keys..." );
	try {
		const result = await postJSON( "/api/invites", {
			count: Number( byID( "count" ).value ),
			maxUses: Number( byID( "uses" ).value ),
			expiresInDays: Number( byID( "days" ).value ),
			note: byID( "note" ).value.trim(),
			...(member ? { discordUserId: member, discordName: byID( "discord-name" ).value.trim() } : {})
		} );
		showMade( result, member );
		setStatus( madeStatus( result ) );
		await load();
	} catch ( error ) {
		setStatus( error.message );
	} finally {
		button.disabled = false;
	}
} );

byID( "made" ).addEventListener( "click", async event => {
	const button = event.target.closest( "[data-copy]" );
	if ( !button ) return;
	await navigator.clipboard.writeText( button.dataset.copy );
	button.textContent = "Copied";
} );

byID( "filter" ).addEventListener( "submit", event => {
	event.preventDefault();
	load();
} );

byID( "rows" ).addEventListener( "click", async event => {
	const button = event.target.closest( ".invites-revoke" );
	if ( !button ) return;
	if ( button.dataset.confirm !== "1" ) {
		button.dataset.confirm = "1";
		button.textContent = "Click again to revoke";
		setTimeout( () => {
			button.dataset.confirm = "";
			button.textContent = "Revoke";
		}, CONFIRM_MS );
		return;
	}
	button.disabled = true;
	try {
		await postJSON( `/api/invites/${encodeURIComponent( button.dataset.id )}/revoke`, {} );
		setStatus( "Key revoked." );
		await load();
	} catch ( error ) {
		setStatus( error.message );
		button.disabled = false;
	}
} );

/*
================
start
================
*/
async function start() {
	try {
		const consoleInfo = await requestJSON( "/api/console" );
		byID( "operator" ).textContent = "Operator: " + consoleInfo.operator;
		if ( !consoleInfo.invites ) {
			setStatus( "Invite keys are not configured on this console (siteAdmin in config.json)." );
			byID( "create-button" ).disabled = true;
			return;
		}
	} catch ( error ) {
		setStatus( error.message );
		return;
	}
	await load();
}

start();
