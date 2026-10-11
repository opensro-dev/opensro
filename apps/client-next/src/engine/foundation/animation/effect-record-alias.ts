/*
===========================================================================

effect-record-alias.ts - Basic_Original aliases in the published effect records

The native client draws a skill through its Basic_Original's record:
GlobalEffectManager_FindSkillMotionMetadata (917240) remaps a positive skill
id once, through skilldata column 6, before the record lookup. A pet's
levelled attacks (PSKILL_P_WOLF_002_ATTACK_01 and on) have no record of
their own and draw through PSKILL_P_WOLF_01_ATTACK01. The published table
stores such a skill as { remapOf: original } instead of a copy of the
record; every reader resolves it here, after decoding the originals once.

===========================================================================
*/

/*
================
effectRecordAlias

The original id an alias row names, or undefined for an authored record.
An alias carries nothing else.
================
*/
export function effectRecordAlias( row: unknown ): number | undefined {
	const alias = row as { remapOf?: unknown; } | null;
	if ( !alias || typeof alias !== "object" || alias.remapOf === undefined ) return undefined;
	if (
		Object.keys( alias ).length !== 1 || !Number.isInteger( alias.remapOf ) || (alias.remapOf as number) <= 0
	) throw Error( "Invalid effect record alias" );
	return alias.remapOf as number;
}

/*
================
resolveEffectRecordAliases

Points each alias of a decoded table at its original's decoded record. The
remap is one step (917240 does not follow a second one), so an alias of an
alias, or of an id with no record, is a broken asset.
================
*/
export function resolveEffectRecordAliases<T>(
	source: Record<string, unknown>,
	decoded: Record<string, T>
): Record<string, T> {
	for ( const [id, row] of Object.entries( source ) ) {
		const original = effectRecordAlias( row );
		if ( original === undefined ) continue;
		const record = decoded[String( original )];
		if ( record === undefined || effectRecordAlias( source[String( original )] ) !== undefined ) {
			throw Error( "Invalid effect record alias target" );
		}
		decoded[id] = record;
	}
	return decoded;
}
