/*
===========================================================================

playerNotes.mjs - the catalogue's notes.json: player-facing patch notes

A release's notes are the "Player-note:" trailers of the changes merged
since the previous release. Each trailer is "Player-note: Area: one plain
sentence"; a change with none is not one a player notices. A note whose
area is not one of the forum's tags, or that names none, is filed under
Other with its text whole: a fix players should hear about is never
dropped, nor a word cut, over its label. The notes are read from main's
first-parent squash commits, whose message GitHub builds from the pull
request's reviewed commits and which cannot change after the merge: a
pull request description stays editable after review, so it is never the
source of public text. No network is involved.

A later change withdraws an earlier note with "Player-note-retract: #<pr>"
(several numbers may share the line): every note of that pull request in
the release's range is left out, so a note a later change made false
never ships. The retracting change may carry its own corrected note.

===========================================================================
*/
import { execFileSync } from "node:child_process";

// NOTE_AREAS are the areas a note may name: the bug forum's own tags.
export const NOTE_AREAS = [ "UI", "Party", "Combat", "Items", "Animation", "Audio", "World", "Other" ];

// NOTE_TRAILER is one note line of a commit message; NOTE_LABEL is the
// optional "Area:" that opens its body (its sentence may be empty).
const NOTE_TRAILER = /^\s*\*?\s*Player-note:\s*(.+)$/i;
// NOTE_RETRACT withdraws earlier notes by pull request number.
const NOTE_RETRACT = /^\s*\*?\s*Player-note-retract:\s*(.+)$/i;
const NOTE_LABEL = /^([A-Za-z]+)\s*:\s*(.*)$/;

// NOTE_FALLBACK_AREA files a note with an unknown area or none.
const NOTE_FALLBACK_AREA = "Other";

// NOTE_MAX_LENGTH bounds one note sentence.
const NOTE_MAX_LENGTH = 400;

// Record and field separators of the git log format below.
const RECORD = "\x1e", FIELD = "\x1f";

/*
================
parsePlayerNotes

The notes of one commit message: every "Player-note:" line. An "Area:"
label in the forum's tags names the area and is taken off the sentence;
any other label, or none, files the note under Other with its text whole,
since "Storage:" and "Warning:" read alike and the word may be the
sentence's own. A label with no sentence after it is no note. Duplicate
lines (a note repeated in two branch commits) count once.
================
*/
export function parsePlayerNotes( message ) {
	const notes = [], seen = new Set();
	for ( const line of String( message ?? "" ).split( /\r?\n/ ) ) {
		const match = NOTE_TRAILER.exec( line );
		if ( !match ) continue;
		const body = match[1].trim(), label = NOTE_LABEL.exec( body );
		const known = label && NOTE_AREAS.find( a => a.toLowerCase() === label[1].toLowerCase() );
		const area = known || NOTE_FALLBACK_AREA;
		const text = (known ? label[2] : body).trim();
		if ( !text || (label && !label[2].trim()) || text.length > NOTE_MAX_LENGTH || seen.has( area + "\n" + text ) ) {
			continue;
		}
		seen.add( area + "\n" + text );
		notes.push( { area, text } );
	}
	return notes;
}

/*
================
parsePlayerNoteRetractions

The pull request numbers a commit message's "Player-note-retract:" lines
withdraw. A line naming no "#<number>" withdraws nothing.
================
*/
export function parsePlayerNoteRetractions( message ) {
	const numbers = new Set();
	for ( const line of String( message ?? "" ).split( /\r?\n/ ) ) {
		const match = NOTE_RETRACT.exec( line );
		if ( !match ) continue;
		for ( const pr of match[1].matchAll( /#(\d+)/g ) ) numbers.add( Number( pr[1] ) );
	}
	return [ ...numbers ].sort( ( a, b ) => a - b );
}

/*
================
notesOfMerges

The notes.json entries of squash merges in merge order, each record a
{ date, subject, message }: a merge without "(#<pr>)" in its subject is
not a pull request and adds nothing, and every note of a retracted pull
request is left out.
================
*/
export function notesOfMerges( records ) {
	const entries = [], retracted = new Set();
	for ( const { date, subject, message } of records ) {
		const pr = /\(#(\d+)\)\s*$/.exec( subject ?? "" );
		if ( !pr ) continue;
		for ( const number of parsePlayerNoteRetractions( message ) ) retracted.add( number );
		for ( const note of parsePlayerNotes( message ) ) entries.push( { date, ...note, pr: Number( pr[1] ) } );
	}
	return entries.filter( entry => !retracted.has( entry.pr ) );
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
	return notesOfMerges(
		log.split( RECORD ).slice( 1 ).map( record => {
			const [date, subject, message] = record.split( FIELD );
			return { date, subject, message };
		} )
	);
}
