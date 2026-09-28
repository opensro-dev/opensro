/*
===========================================================================

resources.ts - guide and quest presentation resources

Owns the guide catalogues, authored layouts and derived quest rows. Demand
controls request admission; step continues collecting results when the guide
is hidden. Gameplay eligibility remains outside this presentation owner.

===========================================================================
*/

import { decodeQuestPresentation, type QuestPresentation } from "@/engine/foundation/ui/quest-presentation";
import {
	decodeQuestGuide,
	questGuideRows,
	type QuestGuideCatalog,
	type QuestGuideRow
} from "@/engine/foundation/ui/quest-guide";
import { generalGuideArticles, type GuideArticle } from "@/engine/foundation/ui/guide-catalog";
import type { AssetOwner } from "@/engine/contracts/assets";
import { decodeAuthoredLayout, type AuthoredLayout } from "@/engine/foundation/ui/authored-layout";
import { guideTokens, type GuideToken } from "@/engine/foundation/ui/guide-content";
/*
================
Load
================
*/
type Load = { kind: "idle"; } | { kind: "loading"; id: number; } | { kind: "ready"; value: unknown; } | {
	kind: "failed";
	message: string;
} | { kind: "disposed"; };
/*
================
createGuideResources
================
*/
export function createGuideResources(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	base: string
) {
	const paths = [
		"/assets/cif/layouts/ifgameguide.json",
		"/assets/data/event-guide-catalog.json",
		"/assets/text/texthelp.en.json",
		"/assets/text/textuisystem.en.json",
		"/assets/cif/layouts/ifmentormatch.json",
		"/assets/cif/layouts/ifmentormatchslot.json",
		"/assets/data/questData.json",
		"/assets/cif/layouts/ifggmenu.json"
	];
	const states: Load[] = paths.map( () => ({ kind: "idle" }) );
	let data: {
			questPresentation: QuestPresentation;
			menu: AuthoredLayout;
			quests: QuestGuideCatalog;
			mentor: AuthoredLayout;
			mentorSlot: AuthoredLayout;
			strings: Readonly<Record<string, string>>;
			general: readonly GuideArticle[];
			caption: string;
			layout: AuthoredLayout;
			articles: readonly {
				id: number;
				title: string;
				tokens: readonly GuideToken[];
				european: readonly GuideToken[] | null;
			}[];
		} | null = null,
		error: string | null = null;
	let questKey = "", questRows: readonly QuestGuideRow[] = [];
	return {
		/*
		================
		quests
		================
		*/
		quests( level: number, active: readonly number[], completed: readonly number[] ) {
			if ( !data ) return [];
			const key = JSON.stringify( [ level, active, completed ] );
			if ( key !== questKey ) {
				questRows = questGuideRows( data.quests, level, active, completed );
				questKey = key;
			}
			return questRows;
		},
		/*
		================
		step

		Demand gates admission only. Always collect previously admitted work.
		================
		*/
		step( needed = true ) {
			let changed = false;
			for ( let i = 0; i < states.length; i++ ) {
				const s = states[i]!;
				if ( s.kind === "loading" ) {
					const r = assets.take( s.id );
					if ( r ) {
						changed = true;
						try {
							if ( r.kind !== "bytes" ) throw Error( "Guide metadata unavailable" );
							states[i] = {
								kind: "ready",
								value: JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( r.buffer ) )
							};
						} catch ( e ) {
							states[i] = { kind: "failed", message: String( e ) };
							error = String( e );
						}
					}
				} else if ( needed && s.kind === "idle" && assets.available() > 0 ) {
					states[i] = { kind: "loading", id: assets.request( new URL( paths[i]!, base ).href, 4 << 20 ) };
				}
			}
			if ( !data && !error && states.every( s => s.kind === "ready" ) ) {
				try {
					const values = states.map( s => s.kind === "ready" ? s.value : null ),
						layout = decodeAuthoredLayout( values[0] );
					const catalog = values[1] as {
							eventRowsByState?: Record<
								string,
								{
									id?: unknown;
									menuKey?: unknown;
									englishContent?: unknown;
									englishEuropeanContent?: unknown;
								}
							>;
						},
						help = values[2] as { entries?: Record<string, unknown>; };
					if ( !catalog?.eventRowsByState || !help?.entries ) throw Error( "Invalid guide catalogs" );
					const articles = Object.entries( catalog.eventRowsByState ).map( ( [key, row] ) => {
						const id = Number( key );
						if (
							!Number.isInteger( id ) || id < 1 || id > 21 || row.id !== 50000 + id ||
							typeof row.menuKey !== "string" || typeof row.englishContent !== "string" ||
							typeof help.entries![row.menuKey] !== "string"
						) throw Error( "Invalid guide article" );
						const europeanKey = id === 1 ?
							"SRO_GGW_EVE_WELCOME_EUROPE" :
							id === 3 ?
							"SRO_GGW_EVE_WEARARMOR_EUROPE" :
							id === 13 ?
							"SRO_GGW_EVE_JOBCHOICE_EUROPE" :
							null;
						const european = europeanKey ? row.englishEuropeanContent : row.englishContent;
						if ( typeof european !== "string" ) throw Error( "Missing European guide branch" );
						return {
							id,
							title: help.entries![row.menuKey] as string,
							tokens: guideTokens( row.englishContent ),
							european: european ? guideTokens( european ) : null
						};
					} );
					if ( articles.length !== 21 || !layout.GDR_GUIDE_DATA_PML || !layout.GDR_GUIDE_PAPER ) {
						throw Error( "Incomplete guide resources" );
					}
					const caption = (values[3] as { entries?: Record<string, unknown>; })?.entries?.UIIT_STT_HELP;
					if ( typeof caption !== "string" || !caption ) throw Error( "Missing guide caption" );
					const strings = (values[3] as { entries: Record<string, string>; }).entries;
					if ( Object.values( strings ).some( v => typeof v !== "string" ) ) {
						throw Error( "Invalid guide text" );
					}
					data = {
						questPresentation: decodeQuestPresentation( values[6] ),
						menu: decodeAuthoredLayout( values[7] ),
						quests: decodeQuestGuide( values[1], values[6] ),
						mentor: decodeAuthoredLayout( values[4] ),
						mentorSlot: decodeAuthoredLayout( values[5] ),
						strings,
						general: generalGuideArticles( values[1], help.entries ),
						caption,
						layout,
						articles
					};
					changed = true;
				} catch ( e ) {
					error = String( e );
					changed = true;
				}
			}
			return changed;
		},
		/*
		================
		data
		================
		*/
		data: () => data, /*
================
error
================
		*/
		error: () => error,
		/*
		================
		dispose

		Release owned children and pending work before discarding local state.
		================
		*/
		dispose() {
			for ( let i = 0; i < states.length; i++ ) {
				const s = states[i]!;
				if ( s.kind === "loading" ) assets.cancel( s.id );
				states[i] = { kind: "disposed" };
			}
			data = null;
			questKey = "";
			questRows = [];
		}
	};
}
