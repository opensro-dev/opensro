/*
===========================================================================

action.mjs - one build step as a content-addressed action

An action is a build step that says what it reads and what it writes:

	name        a stable label (logs, the record), not part of the key
	inputRoot   the folder its inputs are named against
	inputs      input files, relative to inputRoot
	outputRoot  the folder its outputs are written under
	outputs     output files, relative to outputRoot
	code        entry files whose code closure runs it (codeStamp.mjs)
	env         names of the environment variables that change its output
	params      plain JSON values that change its output
	deps        names of the actions whose outputs it reads
	link        materialize outputs as hard links (see store.mjs)
	discoverRoots  { name: folder }: the trees a step that finds its own
	            inputs while it runs may read (reads.mjs); see below
	run         async ( context ) => writes every declared output

The key is the sha256 over the input bytes (by relative name), the code
closure hashes, the named environment values and the params. Paths are
named relative to their roots, so an action keyed in one worktree hits in
another, and outputs materialize under whichever outputRoot asks for them.
Freshness is content, never mtime: a checkout or a rewrite that only
touches a file's time keeps its key (the file hash cache re-reads it and
finds the same bytes), a one-byte change misses.

What the key cannot see is the caller's to declare: a file the step reads
but does not list, a variable it reads but does not name. An undeclared
read is the stale-reuse bug this exists to prevent, as codeStamp.mjs's
closure rule says for code.

A step whose inputs are only known once it runs (a model that reads the
files its BSR lists) declares discoverRoots instead of listing them. Its
key covers what it does declare; the files it read are recorded beside
its outputs, named by root and relative path, and the record is reused
only while each still holds the recorded bytes (store.mjs readRecord).
A discovered read outside every root fails the step: a path the record
cannot name in another worktree could not be checked there.

===========================================================================
*/
import path from "node:path";
import { sha256Hex } from "../shared/hash.mjs";

export const ACTION_KEY_FORMAT = "sro-action-key";
// v1: inputs by relative name, code closures, env values, params.
export const ACTION_KEY_VERSION = 1;
// A missing input is keyed as this digest, so its later appearance misses.
export const MISSING_DIGEST = "missing";

/*
================
defineAction

Checks an action's shape and returns it with every list normalized
(relative names in forward-slash form, sorted, unique). A name that
leaves its root is refused: the key could not tell it from another
worktree's file of the same name.
================
*/
export function defineAction( spec ) {
	if ( !spec || typeof spec.name !== "string" || !spec.name ) throw new Error( "an action needs a name" );
	if ( typeof spec.run !== "function" ) throw new Error( `action ${spec.name}: run must be a function` );
	if ( !spec.outputRoot ) throw new Error( `action ${spec.name}: an outputRoot is required` );
	const outputs = relativeNames( spec.name, "output", spec.outputs ?? [] );
	if ( outputs.length === 0 ) throw new Error( `action ${spec.name}: declares no outputs` );
	const inputs = relativeNames( spec.name, "input", spec.inputs ?? [] );
	if ( inputs.length > 0 && !spec.inputRoot ) throw new Error( `action ${spec.name}: inputs need an inputRoot` );
	return {
		name: spec.name,
		inputRoot: spec.inputRoot ? path.resolve( spec.inputRoot ) : undefined,
		inputs,
		outputRoot: path.resolve( spec.outputRoot ),
		outputs,
		code: [ ...new Set( (spec.code ?? []).map( entry => path.resolve( entry ) ) ) ].sort(),
		env: [ ...new Set( spec.env ?? [] ) ].sort(),
		params: spec.params ?? null,
		deps: [ ...new Set( spec.deps ?? [] ) ],
		link: spec.link === true,
		discoverRoots: discoverRoots( spec.name, spec.discoverRoots ),
		run: spec.run
	};
}

/*
================
discoverRoots

The named trees a step may discover reads in, as absolute folders; null
when the step declares none.
================
*/
function discoverRoots( name, roots ) {
	if ( roots === undefined || roots === null ) return null;
	const entries = Object.entries( roots );
	if ( entries.length === 0 ) throw new Error( `action ${name}: discoverRoots names no root` );
	return Object.freeze( Object.fromEntries( entries.map( ( [root, folder] ) => {
		if ( typeof folder !== "string" || !folder ) {
			throw new Error( `action ${name}: discover root ${root} has no folder` );
		}
		return [ root, path.resolve( folder ) ];
	} ) ) );
}

/*
================
discoveredName

A discovered read as [root, relative name], or null when it lies outside
every root of the action.
================
*/
export function discoveredName( action, file ) {
	const absolute = path.resolve( file );
	for ( const [root, folder] of Object.entries( action.discoverRoots ?? {} ) ) {
		const relative = path.relative( folder, absolute );
		if ( relative && !relative.startsWith( ".." ) && !path.isAbsolute( relative ) ) {
			return [ root, relative.split( path.sep ).join( "/" ) ];
		}
	}
	return null;
}

/*
================
relativeNames
================
*/
function relativeNames( name, kind, list ) {
	const names = new Set();
	for ( const entry of list ) {
		const normalized = path.posix.normalize( String( entry ).replaceAll( "\\", "/" ) );
		if ( !normalized || normalized === "." || normalized.startsWith( "../" ) || path.isAbsolute( normalized ) ) {
			throw new Error( `action ${name}: ${kind} "${entry}" must be a name inside its root` );
		}
		names.add( normalized );
	}
	return [ ...names ].sort();
}

/*
================
inputPath / outputPath
================
*/
export function inputPath( action, name ) {
	return path.join( action.inputRoot, ...name.split( "/" ) );
}

export function outputPath( action, name, outputRoot = action.outputRoot ) {
	return path.join( outputRoot, ...name.split( "/" ) );
}

/*
================
actionKey

The action's key. hashInput( absolutePath ) returns a file's sha256 or
undefined when it is missing (store.mjs passes the file hash cache);
codeHashOf( entry ) the entry's closure hash (codeStamp.mjs codeHash).
env is the environment the values are read from.
================
*/
export async function actionKey( action, { hashInput, codeHashOf, env = process.env } ) {
	const inputs = [];
	for ( const name of action.inputs ) {
		inputs.push( [ name, (await hashInput( inputPath( action, name ) )) ?? MISSING_DIGEST ] );
	}
	const code = [];
	for ( const entry of action.code ) code.push( await codeHashOf( entry ) );
	const environment = action.env.map( variable => [ variable, env[variable] ?? null ] );
	return sha256Hex( JSON.stringify( {
		format: ACTION_KEY_FORMAT,
		version: ACTION_KEY_VERSION,
		inputs,
		code,
		environment,
		params: action.params,
		outputs: action.outputs
	} ) );
}
