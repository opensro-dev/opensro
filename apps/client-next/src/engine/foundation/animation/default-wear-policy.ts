/*
===========================================================================

default-wear-policy.ts - the regional presentation flags of the login shard

744C50 records whether the gateway's shard name carries the case-sensitive
#$T marker. 745D10 (CPSOuterInterface_SubmitShardLogin) turns it into three
GameConfig bytes only when the client language (+0x138) is 0, Korean:
+0x129, +0x12D and +0x12E are all 1 without the marker and 0 with it. Every
other language leaves them at their zero-initialized .data value. +0x129
freezes default wear (8EA2F0); +0x12E selects the authored blood over the
green one (8D5631) and forces the characterInfo death model (8E655D).

===========================================================================
*/

const NATIVE_LANGUAGE_KOREAN = 0;
const NATIVE_LANGUAGE_LAST = 5;
const SHARD_MARKER = "#$T";

/*
================
uncensoredShard

The value 745D10 writes to GameConfig +0x129/+0x12D/+0x12E for this shard.
================
*/
export function uncensoredShard( language: number, rawServerName: string | undefined ): boolean {
	if ( !Number.isInteger( language ) || language < 0 || language > NATIVE_LANGUAGE_LAST ) {
		throw Error( "Invalid native clothing language" );
	}
	if ( language !== NATIVE_LANGUAGE_KOREAN ) return false;
	if ( rawServerName === undefined ) throw Error( "Missing native shard name for clothing policy" );
	return !rawServerName.includes( SHARD_MARKER );
}

/*
================
defaultWearFrozen

8EA2F0 returns before refreshing default wear while +0x129 is set. This
freezes existing default handles, not the rest of the compound.
================
*/
export function defaultWearFrozen( language: number, rawServerName: string | undefined ): boolean {
	return uncensoredShard( language, rawServerName );
}

/*
================
refreshDefaultWear
================
*/
export function refreshDefaultWear(
	previous: readonly string[],
	desired: readonly string[],
	frozen: boolean
): readonly string[] {
	return frozen ? previous : desired;
}
