/*
===========================================================================

tooltip-description.ts - native item PML converted to tooltip rows

Paragraph boundaries share the guide parser. Inline style remains explicit
and unsupported mixed spans fail instead of losing authored formatting.

===========================================================================
*/
import { guideTokens } from "./guide-content";
import type { TooltipRow } from "./tooltip-rows";
// The shipped item descriptions use SML2, BR, STRONG and FONT scopes.
// Preserve authored line breaks, emphasis and ARGB; markup never reaches text.
/*
================
tooltipDescription
================
*/
export function tooltipDescription( source: string ): TooltipRow[] {
	if ( !source || source === "xxx" ) return [];
	if ( !/^<sml2>/i.test( source ) ) return [ { value: source, color: 0xffffffff } ];
	const rows: TooltipRow[] = [];
	let current: TooltipRow | undefined;
	for ( const token of guideTokens( source ) ) {
		if ( token.kind === "image" ) throw Error( "Unsupported image in item tooltip description" );
		if ( token.kind === "paragraph" ) {
			if ( current ) rows.push( current );
			current = undefined;
			continue;
		}
		if ( token.kind === "break" ) {
			rows.push( current ?? { value: " ", color: 0 } );
			current = undefined;
			continue;
		}
		const c = token.color,
			color = c ?
				((Math.round( c[3] * 255 ) << 24) | (Math.round( c[0] * 255 ) << 16) | (Math.round( c[1] * 255 ) << 8) |
					Math.round( c[2] * 255 )) >>> 0 :
				0xffffffff;
		if ( current && (current.color !== color || current.strong !== token.strong) ) {
			throw Error( "Mixed inline item tooltip styles require a span layout" );
		}
		current = { value: (current?.value ?? "") + token.value, color, strong: token.strong };
	}
	if ( current ) rows.push( current );
	return rows;
}
