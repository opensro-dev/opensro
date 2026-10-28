/*
===========================================================================
invites.js - the Invite keys page: make, find and revoke the website's keys

The website stores hashes, so a key's code is shown in full in the
answer that made it (the made-keys panel). A console-made, non-member
key also keeps its code encrypted on the website, so its row can Reveal
it again - every reveal is audited there under the operator's name. A
key for a Discord member is a grant the member collects from the bot, so
it shows no code at all (invite-made.js), and neither does a key made
before codes were kept.
Revoking takes two clicks on the same button. Nothing here sees the
console's secret: the page talks to /api/invites on this console.
===========================================================================
*/
import { createRequest, madeStatus } from "./invite-made.js";
import { allStatusCount, openCount } from "./invite-counts.js";
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

const STATUS_FILTERS = [
	[ "", "All" ],
	[ "unused", "Unused" ],
	[ "partial", "Partly used" ],
	[ "used", "Used" ],
	[ "revoked", "Revoked" ],
	[ "expired", "Expired" ]
];

// The rows on screen and the counts the last load answered with. Revealed
// codes live only here and are dropped on every load: re-revealing is one
// click, and a code never lingers on screen across refreshes.
let current = [];
let currentCounts = null;
const revealed = new Map();

/*
================
keyCellHTML

The key's cell: its last four by default, the whole code while revealed,
and the buttons a kept code earns. A member's key and a key made before
codes were kept have no code to offer.
================
*/
function keyCellHTML( key ) {
	const code = revealed.get( key.id );
	const shown = code ? `<code class="invites-code">${escape( code )}</code>` : `…${escape( key.hint )}`;
	if ( !key.revealable ) {
		const why = key.discordUserId ? "via the bot" : "code not kept";
		return `${shown} <span class="muted">(${why})</span>`;
	}
	const toggle = code ? "Hide" : "Reveal";
	return `${shown}
		<button class="button invites-reveal" type="button" data-id="${escape( key.id )}">${toggle}</button>
		<button class="button invites-copy" type="button" data-id="${escape( key.id )}">Copy</button>`;
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
		<td class="invites-hint">${keyCellHTML( key )}</td>
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
dayStart

A date field's value as an instant `days` later, for the [from, to)
windows the website filters by. Empty stays empty.
================
*/
function dayStart( value, days = 0 ) {
	if ( !value ) return "";
	const at = new Date( value + "T00:00:00.000Z" );
	at.setUTCDate( at.getUTCDate() + days );
	return at.toISOString();
}

/*
================
filters

The filter form as the query the console and the website understand.
================
*/
function filters() {
	const member = byID( "filter-id" ).value.trim();
	const chosen = {
		discordUserId: member,
		status: byID( "filter-status" ).value,
		source: byID( "filter-source" ).value,
		createdFrom: dayStart( byID( "filter-created-from" ).value ),
		createdTo: dayStart( byID( "filter-created-to" ).value, 1 ),
		expiresFrom: dayStart( byID( "filter-expires-from" ).value ),
		expiresTo: dayStart( byID( "filter-expires-to" ).value, 1 )
	};
	const params = new URLSearchParams();
	for ( const [name, value] of Object.entries( chosen ) ) {
		if ( value ) params.set( name, value );
	}
	return { member, query: params.toString() ? "?" + params : "" };
}

/*
================
syncFilterOptions

The counts written into the filter choices: each status and source
carries how many keys choosing it would show (the website counts them
under the other filters). A choice the counts no longer offer stays
listed while it is the chosen one, so the form never lies about what is
being asked.
================
*/
function syncFilterOptions() {
	if ( !currentCounts ) return;
	const status = byID( "filter-status" );
	const keptStatus = status.value;
	status.innerHTML = STATUS_FILTERS.map( ( [value, label] ) => {
		const count = value ? currentCounts.status[value] : allStatusCount( currentCounts );
		return `<option value="${value}">${label} (${count})</option>`;
	} ).join( "" );
	status.value = keptStatus;

	const source = byID( "filter-source" );
	const keptSource = source.value;
	const offered = currentCounts.sources.map( entry =>
		`<option value="${escape( entry.source )}">${escape( entry.source ) || "(no note)"} (${entry.count})</option>`
	);
	source.innerHTML = `<option value="">All sources</option>` + offered.join( "" );
	source.value = keptSource;
	if ( source.value !== keptSource && keptSource ) {
		source.insertAdjacentHTML(
			"beforeend",
			`<option value="${escape( keptSource )}" selected>${escape( keptSource )} (0)</option>`
		);
	}
}

/*
================
render

The table and the status line for what the last load brought. A website
older than filters answers no counts; the line then falls back to the
rows on screen.
================
*/
function render() {
	byID( "rows" ).innerHTML = current.length ?
		current.map( rowHTML ).join( "" ) :
		`<tr><td colspan="8" class="muted">No keys match.</td></tr>`;
	const member = byID( "filter-id" ).value.trim();
	if ( currentCounts ) {
		const open = openCount( currentCounts, byID( "filter-status" ).value );
		setStatus(
			member ?
				`Discord member ${member}: ${currentCounts.total} keys, ${open} open.` :
				`${currentCounts.total} keys, ${open} open.`
		);
	} else {
		const open = current.filter( key => key.status === "active" ).length;
		setStatus( member ? `Discord member ${member}: ${current.length} keys, ${open} open.` : `${open} open keys.` );
	}
	syncFilterOptions();
}

/*
================
load

The key list under the filter form's query.
================
*/
async function load() {
	try {
		const { query } = filters();
		const answer = await requestJSON( "/api/invites" + query );
		current = answer.invites;
		currentCounts = answer.counts ?? null;
		revealed.clear();
		render();
	} catch ( error ) {
		setStatus( error.message );
	}
}

/*
================
fetchCode

One key's whole code, shown once per call by the website and audited
there under the operator's name.
================
*/
async function fetchCode( id ) {
	const answer = await postJSON( `/api/invites/${encodeURIComponent( id )}/reveal`, {} );
	return answer.code;
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
		const result = await postJSON(
			"/api/invites",
			createRequest( {
				count: byID( "count" ).value,
				maxUses: byID( "uses" ).value,
				expiresInDays: byID( "days" ).value,
				note: byID( "note" ).value,
				discordUserId: member,
				discordName: byID( "discord-name" ).value
			} )
		);
		showMade( result, member );
		setStatus( madeStatus( result ) );
		await load();
	} catch ( error ) {
		setStatus( error.message );
	} finally {
		button.disabled = false;
	}
} );

// A grant is one key: How many is locked at 1 while a member is named.
byID( "discord-id" ).addEventListener( "input", () => {
	const count = byID( "count" ), granting = byID( "discord-id" ).value.trim() !== "";
	count.disabled = granting;
	if ( granting ) count.value = "1";
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

byID( "filter-clear" ).addEventListener( "click", () => {
	for (
		const id of [
			"filter-id",
			"filter-status",
			"filter-source",
			"filter-created-from",
			"filter-created-to",
			"filter-expires-from",
			"filter-expires-to"
		]
	) {
		byID( id ).value = "";
	}
	load();
} );

byID( "rows" ).addEventListener( "click", async event => {
	const revealButton = event.target.closest( ".invites-reveal" );
	if ( revealButton ) {
		const id = revealButton.dataset.id;
		if ( revealed.has( id ) ) {
			revealed.delete( id );
			render();
			return;
		}
		revealButton.disabled = true;
		try {
			revealed.set( id, await fetchCode( id ) );
			render();
		} catch ( error ) {
			setStatus( error.message );
			revealButton.disabled = false;
		}
		return;
	}
	const copyButton = event.target.closest( ".invites-copy" );
	if ( copyButton ) {
		const id = copyButton.dataset.id;
		copyButton.disabled = true;
		try {
			const code = revealed.get( id ) ?? (await fetchCode( id ));
			await navigator.clipboard.writeText( code );
			copyButton.textContent = "Copied";
			setTimeout( () => {
				copyButton.textContent = "Copy";
				copyButton.disabled = false;
			}, 1200 );
		} catch ( error ) {
			setStatus( error.message );
			copyButton.disabled = false;
		}
		return;
	}
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
