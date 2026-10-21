/*
===========================================================================

guide-catalog.ts - the general guide's articles from the native help index

Turns the published help index rows into the guide window's categories and
articles, refusing rows the native guide could not draw. The port adds one
closing line to the controls article (CLOSING_ARTICLE).

===========================================================================
*/
import { guideTokens, type GuideToken } from "./guide-content";

/*
================
GuideArticle
================
*/
export interface GuideArticle {
	readonly id: number;
	readonly title: string;
	readonly tokens: readonly GuideToken[];
	readonly parent: number;
	readonly depth: 0 | 1;
}

// Rows at and above this id are event articles, owned elsewhere.
const FIRST_EVENT_ROW = 50000;
// "Mouse controlling", the first article under Controls.
export const CLOSING_ARTICLE = 3001;
// The port's closing line, published with the native guide art
// (scripts/build/shared/cifResources.mjs PORT_GUIDE_IMAGES).
const CLOSING_IMAGE = "/assets/images/Media_extracted/interface/guide/gd_control.png";

/*
================
articleTokens
================
*/
function articleTokens( id: number, content: string ): readonly GuideToken[] {
	const tokens = guideTokens( content );
	if ( id !== CLOSING_ARTICLE ) return tokens;
	return [
		...tokens,
		{ kind: "break" },
		{ kind: "break" },
		{ kind: "image", path: CLOSING_IMAGE }
	];
}

/*
================
generalGuideArticles
================
*/
export function generalGuideArticles(
	value: unknown,
	text: Readonly<Record<string, unknown>>
): readonly GuideArticle[] {
	const rows = (value as { rows?: unknown; }).rows;
	if ( !Array.isArray( rows ) ) throw Error( "Missing guide index" );
	const articles: GuideArticle[] = [];
	for ( const row of rows ) {
		if ( !row.enabled || row.id >= FIRST_EVENT_ROW ) continue;
		if (
			!Number.isSafeInteger( row.id ) || row.id <= 0 || (row.depth !== 0 && row.depth !== 1) ||
			!Number.isSafeInteger( row.parentOrCount ) || typeof text[row.menuKey] !== "string" ||
			(row.depth === 1 && typeof row.englishContent !== "string")
		) {
			throw Error( "Invalid general guide row " + row.id );
		}
		articles.push( {
			id: row.id,
			parent: row.parentOrCount,
			depth: row.depth,
			title: text[row.menuKey] as string,
			tokens: row.depth === 1 ? articleTokens( row.id, row.englishContent ) : []
		} );
	}
	const ids = new Set( articles.map( a => a.id ) );
	if ( ids.size !== articles.length ) throw Error( "Duplicate guide article" );
	for ( const a of articles ) {
		if ( a.depth === 1 && !articles.some( p => p.depth === 0 && p.id === a.parent ) ) {
			throw Error( "Orphan guide article" );
		}
		if ( a.depth === 0 && articles.filter( c => c.depth === 1 && c.parent === a.id ).length !== a.parent ) {
			throw Error( "Guide category count mismatch" );
		}
	}
	return articles;
}
