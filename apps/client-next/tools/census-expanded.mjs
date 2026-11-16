/*
===========================================================================

census-expanded.mjs - the action census's rule on an expanded build

An expanded build (scripts/lib/expandedBuild.mjs: the Alexandria variant,
Arabia's map guide rows) publishes more controls than the native census
records. There the check (ui-action-census.mjs) requires every committed
scene, control and row to survive and lets added ones pass, as the other
native censuses do (#3596).

===========================================================================
*/

// A row's id lists: an expanded build may append ids, never drop one.
const ID_LISTS = new Set( [ "added", "removed" ] );
// The row's digest of its own contents, so it moves whenever a list grows.
const ROW_DIGEST = "hash";

/*
================
rowSurvives

True when the built row keeps everything the native row records: every
field equal except the id lists, which must hold every native id, and the
digest, which follows the lists. An id a list gains must be one the expanded
data adds: an action that now opens or closes a native control changed
native behaviour.
================
*/
function rowSurvives( native, built, nativeControls ) {
	if ( native === null || built === null ) return native === built;
	for ( const [key, value] of Object.entries( native ) ) {
		if ( key === ROW_DIGEST ) continue;
		if ( ID_LISTS.has( key ) && Array.isArray( value ) && Array.isArray( built[key] ) ) {
			const ids = new Set( built[key] ), kept = new Set( value );
			if ( value.some( id => !ids.has( id ) ) ) return false;
			if ( built[key].some( id => !kept.has( id ) && nativeControls.has( id ) ) ) return false;
			continue;
		}
		if ( JSON.stringify( value ) !== JSON.stringify( built[key] ) ) return false;
	}
	return true;
}

/*
================
lostNativeRow

On an expanded build, the first committed census fact the built table no
longer holds: a scene, its panel or focus, a control id, or a row that lost
a native fact (rowSurvives). Undefined when every native fact survives;
controls, rows and row ids the expanded data adds are not differences.
================
*/
export function lostNativeRow( before, table, canonical ) {
	const nativeControls = new Set(
		Object.values( before.scenes ?? {} ).flatMap( scene => scene.controls ?? [] )
	);
	for ( const [name, scene] of Object.entries( before.scenes ?? {} ) ) {
		const built = table.scenes[name];
		if ( !built ) return `scene ${name} is gone`;
		if ( scene.panel !== built.panel || scene.focus !== built.focus ) return `scene ${name} opens differently`;
		const controls = new Set( built.controls );
		const control = (scene.controls ?? []).find( id => !controls.has( id ) );
		if ( control !== undefined ) return `scene ${name} lost control ${control}`;
		for ( const [id, row] of Object.entries( scene.rows ?? {} ) ) {
			if ( !(id in built.rows) ) return `scene ${name} lost row ${id}`;
			if ( !rowSurvives( canonical( row ), built.rows[id], nativeControls ) ) {
				return `scene ${name} row ${id} changed`;
			}
		}
	}
	return undefined;
}
