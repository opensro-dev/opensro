/*
===========================================================================

beginnerQuests.mjs - display projection for the optional beginner quest track

Port-only, not native. The Go quest owner embeds the same authored JSON.
Display records are always available for retained journals after a switch
is disabled; only the server's flag admits offers, progress and rewards.
These rows are deliberately absent from the native quest guide.

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { serverSourceRoot } from "../world/paths.mjs";

const ROUTE_STRIDE = 100;
const WEAPON_NAMES = {
	CH_SWORD: "Chinese sword",
	CH_BLADE: "Chinese blade",
	CH_SPEAR: "Chinese spear",
	CH_TBLADE: "Chinese glaive",
	CH_BOW: "Chinese bow",
	EU_SWORD: "European one-handed sword",
	EU_TSWORD: "European two-handed sword",
	EU_AXE: "European dual axes",
	EU_DAGGER: "European daggers",
	EU_CROSSBOW: "European crossbow",
	EU_TSTAFF: "European staff",
	EU_DARKSTAFF: "European dark staff",
	EU_STAFF: "European cleric rod",
	EU_HARP: "European harp"
};

/*
================
readBeginnerQuestContent
================
*/
export function readBeginnerQuestContent() {
	return JSON.parse( fs.readFileSync(
		path.join( serverSourceRoot(), "internal/game/quest/beginner_quests.json" ),
		"utf8"
	) );
}

/*
================
beginnerQuestProjection

Return a detached additive asset projection; publishing and pack ownership
remain with the existing quest-data builder.
================
*/
export function beginnerQuestProjection( content = readBeginnerQuestContent() ) {
	const rows = [];
	const textEntries = {};
	for ( const [route, race] of [ "CH", "EU" ].entries() ) {
		for ( const m of content.milestones ) {
			const code = `QPORT_BEGINNER_${race}_${String( m.level ).padStart( 2, "0" )}`;
			const title = m.degree > 0 ?
				`Beginner: Degree ${m.degree} Moon Weapon (Lv. ${m.level})` :
				`Beginner: Supply Patrol (Lv. ${m.level})`;
			const target = race === "CH" ? m.chName : m.euName;
			const town = m.level >= 42 ? "Hotan" : m.level >= 24 ?
				(race === "CH" ? "Donwhang" : "Samarkand") :
				(race === "CH" ? "Jangan" : "Constantinople");
			const objective = `Defeat ${m.kills} ${target} and return to the potion merchant in ${town}.`;
			const binding =
				"All item rewards are permanently character-bound. They cannot be traded, sold, dropped, stored in shared storage or dissolved.";
			const reward = m.degree > 0 ?
				`Choose one degree ${m.degree} Seal of Moon weapon, Chinese or European.` :
				`${m.potions} HP potions and ${m.potions} MP potions.`;
			textEntries[`SN_${code}`] = title;
			textEntries[`SN_PAY_${code}`] = title;
			textEntries[`SN_PAYCON_${code}`] = `${m.exp} EXP\n${m.skillExp} skill EXP\n${reward}\n${binding}`;
			textEntries[`SN_NN_${code}`] = `Potion merchant (${town})`;
			textEntries[`SN_NC_${code}`] = objective;
			textEntries[`SN_CON_${code}`] = `${target}: %d / ${m.kills}`;
			textEntries[`SN_TALK_${code}_OFFER`] =
				`${objective}\n\n${reward}\n${binding}\nAvailable once per character.`;
			textEntries[`SN_TALK_${code}_PROGRESS`] = objective;
			textEntries[`SN_TALK_${code}_COMPLETE`] =
				`Your patrol is complete. ${reward}\n${binding}\nYour choice is final.`;
			textEntries[`SN_TALK_${code}_FULL`] =
				"Make room in your inventory, then speak to me again. Your reward has not been claimed.";
			rows.push( [
				content.firstRefId + route * ROUTE_STRIDE + m.level,
				m.level,
				`SN_${code}`,
				`SN_PAY_${code}`,
				`SN_PAYCON_${code}`,
				`SN_NN_${code}`,
				`SN_NC_${code}`
			].join( "\t" ) );
		}
	}
	for ( const m of content.milestones.filter( m => m.degree > 0 ) ) {
		for ( const family of content.weapons ) {
			if ( !WEAPON_NAMES[family] ) throw Error( `Unknown beginner weapon family ${family}` );
			textEntries[`SN_PORT_BEGINNER_${family}_${m.degree}`] = `D${m.degree} Seal of Moon: ${
				WEAPON_NAMES[family]
			} (character-bound)`;
			textEntries[`SN_PORT_BEGINNER_CONFIRM_${family}_${m.degree}`] =
				`Claim one degree ${m.degree} Seal of Moon ${
					WEAPON_NAMES[family]
				}?\nThis weapon is permanently character-bound. It cannot be traded, sold, dropped, stored in shared storage or dissolved.\nYour choice is final.`;
		}
	}
	return { rows, textEntries };
}

/*
================
appendBeginnerQuestProjection

Refuse collisions rather than replacing any media-owned record or string.
================
*/
export function appendBeginnerQuestProjection( catalog ) {
	const content = readBeginnerQuestContent();
	const projection = beginnerQuestProjection( content );
	const ids = new Set( catalog.rows.map( row => Number( row.split( "\t" )[0] ) ) );
	for ( const row of projection.rows ) {
		const id = Number( row.split( "\t" )[0] );
		if ( ids.has( id ) ) throw Error( `Beginner quest ID collision: ${id}` );
		ids.add( id );
		catalog.rows.push( row );
		catalog.giveupWarnBytes.push( 1 );
		catalog.countryBytes.push( id >= content.firstRefId + ROUTE_STRIDE ? 1 : 0 );
		catalog.progressClearBytes.push( 0 );
	}
	for ( const [symbol, text] of Object.entries( projection.textEntries ) ) {
		if ( Object.hasOwn( catalog.textEntries, symbol ) ) throw Error( `Beginner quest text collision: ${symbol}` );
		catalog.textEntries[symbol] = text;
	}
}
