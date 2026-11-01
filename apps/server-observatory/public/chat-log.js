/*
===========================================================================

chat-log.js - the Chat log page: search one shard's moderation log

Reads /api/chatlog (chatlog.mjs forwards the GameWorld's operator route)
and paints the lines newest first. Everything a player typed reaches the
page through textContent, so chat text is never parsed as markup.

===========================================================================
*/
import { requestJSON } from "./request.js";

const byID = id => document.getElementById( id );

/*
================
searchURL

The form as an /api/chatlog request. datetime-local reads as this
browser's local time; the server takes Unix milliseconds.
================
*/
export function searchURL( fields ) {
	const query = new URLSearchParams( { source: fields.source } );
	for ( const name of [ "account", "character" ] ) {
		const value = fields[name].trim();
		if ( value ) query.set( name, value );
	}
	for ( const name of [ "from", "to" ] ) {
		if ( fields[name] ) query.set( name, String( new Date( fields[name] ).getTime() ) );
	}
	return "/api/chatlog?" + query;
}

/*
================
cell
================
*/
function cell( text, className ) {
	const node = document.createElement( "td" );
	node.textContent = text;
	if ( className ) node.className = className;
	return node;
}

/*
================
paint
================
*/
function paint( result ) {
	const rows = result.lines.map( line => {
		const row = document.createElement( "tr" );
		const channel = line.target ? line.channel + " to " + line.target : line.channel;
		row.append(
			cell( new Date( line.ts ).toLocaleString() ),
			cell( line.character ),
			cell( line.account, "chat-account" ),
			cell( line.muted ? channel + " (muted)" : channel ),
			cell( line.text, "chat-text" )
		);
		return row;
	} );
	byID( "chat-rows" ).replaceChildren( ...rows );
	const notes = [ result.lines.length + " lines, newest first." ];
	if ( result.truncated ) notes.push( "Older matches exist: narrow the search or the time range." );
	if ( result.skipped ) notes.push( result.skipped + " unreadable lines were skipped." );
	byID( "chat-status" ).textContent = notes.join( " " );
}

/*
================
search
================
*/
async function search() {
	byID( "chat-status" ).textContent = "Searching…";
	const form = byID( "chat-filters" );
	try {
		paint( await requestJSON( searchURL( Object.fromEntries( new FormData( form ) ) ) ) );
	} catch ( error ) {
		// A failed read must not leave the last search looking current.
		byID( "chat-rows" ).replaceChildren();
		byID( "chat-status" ).textContent = error.message;
	}
}

/*
================
init
================
*/
async function init() {
	const form = byID( "chat-filters" );
	form.addEventListener( "submit", event => {
		event.preventDefault();
		void search();
	} );
	try {
		const config = await requestJSON( "/api/console" );
		byID( "chat-shard" ).replaceChildren( ...config.shards.map( shard => new Option( shard.name, shard.id ) ) );
		await search();
	} catch ( error ) {
		byID( "chat-status" ).textContent = error.message;
	}
}

if ( globalThis.document ) void init();
