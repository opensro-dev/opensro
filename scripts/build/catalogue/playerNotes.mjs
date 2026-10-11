/*
===========================================================================

playerNotes.mjs - the catalogue's notes.json: player-facing patch notes

A release's notes are the "## Player note" sections of the pull requests
merged since the previous release. Each line of the section is
"Area: one plain sentence"; "None" (or no section) means the change is not
one a player notices. Main is squash-merged, so each first-parent commit
whose subject ends in "(#N)" is one pull request, and its description is
read with the GitHub CLI. The template (.github/pull_request_template.md)
asks for public words only, so no security detail reaches the file.

===========================================================================
*/
import { execFileSync } from "node:child_process";

// NOTE_AREAS are the areas a note may name: the bug forum's own tags.
export const NOTE_AREAS = [ "UI", "Party", "Combat", "Items", "Animation", "Audio", "World", "Other" ];

// NOTE_HEADING opens a pull request's player note section.
const NOTE_HEADING = /^##\s+player note\s*$/i;

// NOTE_MAX_LENGTH bounds one note sentence.
const NOTE_MAX_LENGTH = 400;

/*
================
parsePlayerNote

The notes of one pull request description: every "Area: text" line of its
"## Player note" section, up to the next heading. A line that names no
known area, or "None", adds nothing.
================
*/
export function parsePlayerNote( body ) {
	const notes = [];
	let inSection = false;
	for ( const raw of String( body ?? "" ).split( /\r?\n/ ) ) {
		const line = raw.trim();
		if ( NOTE_HEADING.test( line ) ) {
			inSection = true;
			continue;
		}
		if ( !inSection ) continue;
		if ( line.startsWith( "#" ) ) break;
		const match = /^[-*]?\s*([A-Za-z]+)\s*:\s*(.+)$/.exec( line );
		if ( !match ) continue;
		const area = NOTE_AREAS.find( a => a.toLowerCase() === match[1].toLowerCase() );
		const text = match[2].trim();
		if ( area && text && text.length <= NOTE_MAX_LENGTH ) notes.push( { area, text } );
	}
	return notes;
}

/*
================
mergedPullRequests

The pull requests merged on main after since: [{ pr, date }] in merge
order, from the first-parent squash subjects.
================
*/
export function mergedPullRequests( repositoryRoot, since ) {
	const log = execFileSync( "git", [ "log", "--first-parent", "--reverse", "--format=%cs%x09%s", since + "..HEAD" ], {
		cwd: repositoryRoot,
		encoding: "utf8"
	} );
	const out = [];
	for ( const line of log.split( /\r?\n/ ) ) {
		const [date, subject] = line.split( "\t" );
		const match = /\(#(\d+)\)\s*$/.exec( subject ?? "" );
		if ( match ) out.push( { pr: Number( match[1] ), date } );
	}
	return out;
}

/*
================
playerNotes

The notes.json entries since a release commit. Without one the list is
empty: the notes describe a release's difference, never the whole history.
================
*/
export function playerNotes( repositoryRoot, since ) {
	if ( !since ) return [];
	const entries = [];
	for ( const { pr, date } of mergedPullRequests( repositoryRoot, since ) ) {
		const body = execFileSync( "gh", [ "pr", "view", String( pr ), "--json", "body", "--jq", ".body" ], {
			cwd: repositoryRoot,
			encoding: "utf8"
		} );
		for ( const note of parsePlayerNote( body ) ) entries.push( { date, ...note, pr } );
	}
	return entries;
}
