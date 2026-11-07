/*
===========================================================================

companion-potions.ts - the T dialog's companion recovery preferences

Port-only, not native. The existing transport auto-potion opt-in gates both
companions. Settings are browser-local per server and character; the native
player auto-potion packet remains unchanged. One shared item binding per
recovery kind serves both companions through ordinary validated item use.

===========================================================================
*/
import {
	autoPotionDraft,
	autoPotionDraftChoice,
	autoPotionEntry,
	autoPotionWord,
	defaultAutoPotion
} from "@/engine/foundation/gameplay/auto-potion";
import type { AutoPotionDraft } from "@/engine/foundation/gameplay/auto-potion";

export type CompanionPotionChannel = "transport" | "attack" | "vigorHp" | "vigorMp";
export interface CompanionPotionSettings {
	readonly transport: number;
	readonly attack: number;
	readonly vigorHp: number;
	readonly vigorMp: number;
	readonly bindings: AutoPotionDraft;
}
const STORAGE_PREFIX = "sro.companion-potions.v1:";
/*
================
defaultCompanionPotions
================
*/
export function defaultCompanionPotions(): CompanionPotionSettings {
	return {
		vigorHp: autoPotionWord( { enabled: false, percent: 50, slot: 1 } ),
		vigorMp: autoPotionWord( { enabled: false, percent: 50, slot: 1 } ),
		transport: autoPotionWord( { enabled: true, percent: 50, slot: 0 } ),
		attack: autoPotionWord( { enabled: true, percent: 50, slot: 0 } ),
		bindings: autoPotionDraft( { ...defaultAutoPotion(), hp: 0, cure: 0 } )
	};
}
/*
================
createCompanionPotions

Drafts are committed only by OK; character changes and reconnects clear the
retry clock so one session's timestamp cannot suppress another's recovery.
================
*/
export function createCompanionPotions( storage: Pick<Storage, "getItem" | "setItem"> | null ) {
	let scope = "", saved = defaultCompanionPotions(), draft = saved;
	let lastSentMs: number | undefined;
	let tab: "character" | "companion" = "character";
	return {
		/*
 ================
 sync
 ================
 */
		sync( nextScope: string ) {
			if ( scope === nextScope ) return;
			scope = nextScope;
			saved = defaultCompanionPotions();
			lastSentMs = undefined;
			try {
				const raw = scope ? storage?.getItem( STORAGE_PREFIX + scope ) : null;
				if ( raw ) {
					const value = JSON.parse( raw );
					const transport = autoPotionEntry( value.transport ), attack = autoPotionEntry( value.attack );
					if (
						transport.percent < 1 || transport.percent > 100 || attack.percent < 1 || attack.percent > 100
					) throw Error( "Invalid companion HP threshold" );
					const bindings = autoPotionDraft( value.bindings );
					// Validate the complete packed binding rather than trusting stored JSON.
					autoPotionEntry( bindings.hp );
					autoPotionEntry( bindings.cure );
					const defaults = defaultCompanionPotions();
					const vigorHp = value.vigorHp ?? defaults.vigorHp, vigorMp = value.vigorMp ?? defaults.vigorMp;
					for ( const word of [ vigorHp, vigorMp ] ) {
						const entry = autoPotionEntry( word );
						if ( entry.percent < 1 || entry.percent > 100 || entry.slot < 1 ) {
							throw Error( "Invalid vigor preference" );
						}
					}
					saved = { transport: value.transport, attack: value.attack, vigorHp, vigorMp, bindings };
				}
			} catch {
				saved = defaultCompanionPotions();
			}
			draft = saved;
		},
		/*
 ================
 begin
 ================
 */
		begin() {
			draft = saved;
			tab = "character";
		},
		/*
 ================
 state
 ================
 */
		state() {
			return { saved, draft, lastSentMs, tab };
		},
		/*
 ================
 selectTab
 ================
 */
		selectTab( next: "character" | "companion" ) {
			tab = next;
		},
		/*
 ================
 toggle
 ================
 */
		toggle( key: CompanionPotionChannel | "cure" ) {
			if ( key === "cure" ) {
				const entry = autoPotionEntry( draft.bindings.cure );
				draft = {
					...draft,
					bindings: { ...draft.bindings, cure: autoPotionWord( { ...entry, enabled: !entry.enabled } ) }
				};
			} else {
				const entry = autoPotionEntry( draft[key] );
				draft = { ...draft, [key]: autoPotionWord( { ...entry, enabled: !entry.enabled } ) };
			}
		},
		/*
 ================
 percent
 ================
 */
		percent( key: CompanionPotionChannel, value: number ) {
			const entry = autoPotionEntry( draft[key] );
			if ( !entry.enabled || !Number.isInteger( value ) || value < 1 || value > 100 ) return;
			draft = { ...draft, [key]: autoPotionWord( { ...entry, percent: value } ) };
		},
		/*
 ================
 choice
 ================
 */
		choice( key: "hp" | "cure" | "vigorHp" | "vigorMp", part: "page" | "key", value: number ) {
			if (
				!Number.isInteger( value ) || value < (part === "page" ? 0 : 1) || value > (part === "page" ? 3 : 10)
			) return;
			if ( key === "vigorHp" || key === "vigorMp" ) {
				draft = {
					...draft,
					[key]: autoPotionDraftChoice( { ...draft.bindings, hp: draft[key] }, "hp", part, value ).hp
				};
			} else {
				draft = { ...draft, bindings: autoPotionDraftChoice( draft.bindings, key, part, value ) };
			}
		},
		/*
 ================
 save
 ================
 */
		save() {
			saved = draft;
			try {
				if ( scope ) storage?.setItem( STORAGE_PREFIX + scope, JSON.stringify( saved ) );
			} catch { /* Browser storage can be unavailable; this session still works. */ }
		},
		/*
 ================
 sent
 ================
 */
		sent( now: number ) {
			lastSentMs = now;
		}
	};
}
