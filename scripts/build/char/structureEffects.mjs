/*
===========================================================================

structureEffects.mjs - the fortress structures' damage stages (atstructeffect.txt)

CItemVisualScript_ParseCommand (client 4F9880) reads four commands:

	#TARGET	<codename>                              selects a RefObj record
	#BSR	<stage 0..2>	<resource>              the stage's model
	#SOUND	<stage 0..2>	<effect sound>	<shake>  played on reaching the stage
	#EFFECT	<level 0..5>	<efp>	<x>	<y>	<z>	<rotation>	<loop>	<particle>

A command before a resolvable #TARGET is ignored, as the native parser
skips it while its current record is 0. Stage 0 stands, 1 is damaged (60%
of hit points or less) and 2 destroyed; levels 0..5 are the damage decals
CICATStruct_UpdateDamageVisualStage (4F7B30) selects by lost fifths.

===========================================================================
*/

const STAGE_COUNT = 3;
const DAMAGE_LEVEL_COUNT = 6;
const PARTICLE_TYPE_COUNT = 3;

/*
================
integer
================
*/
function integer( cell, label ) {
	const value = Number( cell );
	if ( !Number.isInteger( value ) ) throw new Error( `atstructeffect: invalid ${label} ${JSON.stringify( cell )}` );
	return value;
}

/*
================
finite
================
*/
function finite( cell, label ) {
	const value = Number( cell );
	if ( !Number.isFinite( value ) ) throw new Error( `atstructeffect: invalid ${label} ${JSON.stringify( cell )}` );
	return value;
}

/*
================
resourcePath

Retail paths are backslashed and mixed case; the asset tree is lower case.
================
*/
function resourcePath( cell ) {
	const value = String( cell ?? "" ).trim().replaceAll( "\\", "/" ).toLowerCase();
	if ( !value || value.startsWith( "/" ) || value.includes( ".." ) || value.includes( ":" ) ) {
		throw new Error( `atstructeffect: invalid resource ${JSON.stringify( cell )}` );
	}
	return value;
}

/*
================
parseStructureEffects

Returns codename -> { stages, sounds, levels } for every #TARGET whose
codename `known` admits (the native GetItemRecordByCodeName lookup).
stages[n] is a "res/..." BSR path, sounds[n] { handle, shake }, and
levels[n] the ordered effect list of damage level n.
================
*/
export function parseStructureEffects( text, known ) {
	const targets = new Map();
	let current = null;
	for ( const line of text.replace( /^﻿/, "" ).split( /\r?\n/ ) ) {
		const cells = line.split( "\t" ).map( cell => cell.trim() );
		const command = cells[0];
		if ( command === "#TARGET" ) {
			const codename = cells[1];
			current = codename && known( codename ) ?
				targets.get( codename ) ?? { stages: {}, sounds: {}, levels: {} } :
				null;
			if ( current ) targets.set( codename, current );
			continue;
		}
		if ( !current || ![ "#BSR", "#SOUND", "#EFFECT" ].includes( command ) ) continue;
		const index = integer( cells[1], `${command} index` );
		if ( command === "#BSR" ) {
			if ( index < 0 || index >= STAGE_COUNT ) continue;
			const bsrPath = resourcePath( cells[2] );
			if ( !bsrPath.startsWith( "res/" ) || !bsrPath.endsWith( ".bsr" ) ) {
				throw new Error( `atstructeffect: stage model ${JSON.stringify( cells[2] )} is not a res/ BSR` );
			}
			current.stages[index] = bsrPath;
		} else if ( command === "#SOUND" ) {
			if ( index < 0 || index >= STAGE_COUNT ) continue;
			current.sounds[index] = { handle: cells[2], shake: integer( cells[3], "#SOUND shake" ) > 0 };
		} else {
			// CItemVisualScript_AppendEffect (4F9660) admits levels 0..5;
			// a particle type outside 0..2 reads as 0.
			if ( index < 0 || index >= DAMAGE_LEVEL_COUNT ) continue;
			const particle = integer( cells[8], "#EFFECT particle type" );
			(current.levels[index] ??= []).push( {
				effectPath: resourcePath( cells[2] ),
				offset: [
					finite( cells[3], "#EFFECT x" ),
					finite( cells[4], "#EFFECT y" ),
					finite( cells[5], "#EFFECT z" )
				],
				rotation: integer( cells[6], "#EFFECT rotation" ),
				loop: integer( cells[7], "#EFFECT loop" ) > 0,
				particle: particle >= 0 && particle < PARTICLE_TYPE_COUNT ? particle : 0
			} );
		}
	}
	return targets;
}
