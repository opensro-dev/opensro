/*
===========================================================================

column-heads.ts - English corrections for list column heads

The retail English catalogue translates UIIT_STT_GRADE (Korean 등급,
"grade") as "level". As a suffix in tooltips ("3 level") that reads, but
as a column head it names the wrong column: the guild roster's head over
each member's rank (Leader, Staff, Member) sits beside the real "Level",
and the job ranking's head over the job grade reads "level". The key is
shared with those tooltips, so the catalogue table
(scripts/build/shared/englishCorrections.json) cannot change it; the
heads take the catalogue's own "Grade" (UIIT_CTL_TC_HONOR_CLASS) here.
Like that table this is a text correction, always on, with no row.

===========================================================================
*/
import type { AuthoredLayout } from "./authored-layout";

const COLUMN_HEAD_CORRECTIONS = {
	UIIT_STT_GRADE: "UIIT_CTL_TC_HONOR_CLASS"
} as const;

/*
================
columnHeadKey

The text key a column head shows for its authored key.
================
*/
export function columnHeadKey( key: string ): string {
	return (COLUMN_HEAD_CORRECTIONS as Readonly<Record<string, string>>)[key] ?? key;
}

/*
================
columnHeadLayout

The layout with every node's text key corrected as a column head, for
a page whose heads are drawn as its own statics (the job ranking).
================
*/
export function columnHeadLayout( layout: AuthoredLayout ): AuthoredLayout {
	const out: Record<string, AuthoredLayout[string]> = {};
	for ( const [name, node] of Object.entries( layout ) ) {
		const key = columnHeadKey( node.text );
		out[name] = key === node.text ? node : { ...node, text: key };
	}
	return out;
}
