/*
===========================================================================

census-expanded.mjs - the action census's rule on an expanded build

An expanded build (scripts/lib/expandedBuild.mjs: the Alexandria variant,
Arabia's map guide rows) publishes more controls than the native census
records. There the check (ui-action-census.mjs) requires every committed
scene, control and row to survive and lets added ones pass, as the other
native censuses do (#3596). A native window the build replaces with a later
client's (replaced-windows.mjs: the renewal Item Mall) defines its own
controls; everything outside it still has to survive.

===========================================================================
*/

// A row's id lists: an expanded build may append ids, never drop one.
const ID_LISTS = new Set( [ "added", "removed" ] );
// The row's digest of its own contents, so it moves whenever a list grows.
const ROW_DIGEST = "hash";

/*
================
insideWindow

Whether a control id belongs to the window whose ids start with prefix:
"item-mall-close" and "item-mall-category:4" do; "item-mall", the HUD
button that opens the window, does not, and stays a native control.
================
*/
export function insideWindow( id, prefix ) {
	return id.startsWith( prefix + "-" ) || id.startsWith( prefix + ":" );
}

/*
================
rowSurvives

True when the built row keeps everything the native row records: every
field equal except the id lists, which must hold every native id, and the
digest, which follows the lists. An id a list gains must be one the expanded
data adds: an action that now opens or closes a native control changed
native behaviour. Ids inside a replaced window (replaced) are the replacing
window's, on either side.
================
*/
function rowSurvives( native, built, nativeControls, replaced ) {
	if ( native === null || built === null ) return native === built;
	for ( const [key, value] of Object.entries( native ) ) {
		if ( key === ROW_DIGEST ) continue;
		if ( ID_LISTS.has( key ) && Array.isArray( value ) && Array.isArray( built[key] ) ) {
			const own = built[key].filter( id => !replaced( id ) ), ids = new Set( own ), kept = new Set( value );
			if ( value.some( id => !replaced( id ) && !ids.has( id ) ) ) return false;
			if ( own.some( id => !kept.has( id ) && nativeControls.has( id ) ) ) return false;
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

replacedPrefixes are the control id prefixes of the native windows this
build replaces. The ids inside such a window may go, come or change, as may
the rows they key; the row of the button that opens it is still checked.
================
*/
export function lostNativeRow( before, table, canonical, replacedPrefixes = [] ) {
	const replaced = id => replacedPrefixes.some( prefix => insideWindow( id, prefix ) );
	const nativeControls = new Set(
		Object.values( before.scenes ?? {} ).flatMap( scene => scene.controls ?? [] )
	);
	for ( const [name, scene] of Object.entries( before.scenes ?? {} ) ) {
		const built = table.scenes[name];
		if ( !built ) return `scene ${name} is gone`;
		if ( scene.panel !== built.panel || scene.focus !== built.focus ) return `scene ${name} opens differently`;
		const controls = new Set( built.controls );
		const control = (scene.controls ?? []).find( id => !replaced( id ) && !controls.has( id ) );
		if ( control !== undefined ) return `scene ${name} lost control ${control}`;
		for ( const [id, row] of Object.entries( scene.rows ?? {} ) ) {
			if ( replaced( id ) ) continue;
			if ( !(id in built.rows) ) return `scene ${name} lost row ${id}`;
			if ( !rowSurvives( canonical( row ), built.rows[id], nativeControls, replaced ) ) {
				return `scene ${name} row ${id} changed`;
			}
		}
	}
	return undefined;
}
