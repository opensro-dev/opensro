/*
===========================================================================

playerNotes.mjs - the catalogue's notes.json: player-facing patch notes

A release's notes are the "Player-note:" trailers of the changes merged
since the previous release. Each trailer is "Player-note: Area: one plain
sentence"; a change with none is not one a player notices. The notes are
read from main's first-parent squash commits, whose message GitHub builds
from the pull request's reviewed commits and which cannot change after the
merge: a pull request description stays editable after review, so it is
never the source of public text. No network is involved.

===========================================================================
*/
import { execFileSync } from "node:child_process";

// NOTE_AREAS are the areas a note may name: the bug forum's own tags.
export const NOTE_AREAS = [ "UI", "Party", "Combat", "Items", "Animation", "Audio", "World", "Other" ];

// NOTE_TRAILER is one note line of a commit message.
const NOTE_TRAILER = /^\s*\*?\s*Player-note:\s*([A-Za-z]+)\s*:\s*(.+)$/i;

// NOTE_MAX_LENGTH bounds one note sentence.
const NOTE_MAX_LENGTH = 400;

// Record and field separators of the git log format below.
const RECORD = "\x1e", FIELD = "\x1f";

/*
================
parsePlayerNotes

The notes of one commit message: every "Player-note: Area: text" line in
a known area. Duplicate lines (a note repeated in two branch commits) count
once.
================
*/
export function parsePlayerNotes( message ) {
	const notes = [], seen = new Set();
	for ( const line of String( message ?? "" ).split( /\r?\n/ ) ) {
		const match = NOTE_TRAILER.exec( line );
		if ( !match ) continue;
		const area = NOTE_AREAS.find( a => a.toLowerCase() === match[1].toLowerCase() );
		const text = match[2].trim();
		if ( !area || !text || text.length > NOTE_MAX_LENGTH || seen.has( area + "\n" + text ) ) continue;
		seen.add( area + "\n" + text );
		notes.push( { area, text } );
	}
	return notes;
}

/*
================
playerNotes

The notes.json entries of the squash merges after since, in merge order.
Without since the list is empty: the notes describe a release's difference,
never the whole history.
================
*/
export function playerNotes( repositoryRoot, since ) {
	if ( !since ) return [];
	const log = execFileSync( "git", [
		"log",
		"--first-parent",
		"--reverse",
		"--format=" + RECORD + "%cs" + FIELD + "%s" + FIELD + "%B",
		since + "..HEAD"
	], { cwd: repositoryRoot, encoding: "utf8", maxBuffer: 64 << 20 } );
	const entries = [];
	for ( const record of log.split( RECORD ).slice( 1 ) ) {
		const [date, subject, message] = record.split( FIELD );
		const pr = /\(#(\d+)\)\s*$/.exec( subject ?? "" );
		if ( !pr ) continue;
		for ( const note of parsePlayerNotes( message ) ) entries.push( { date, ...note, pr: Number( pr[1] ) } );
	}
	return entries;
}
