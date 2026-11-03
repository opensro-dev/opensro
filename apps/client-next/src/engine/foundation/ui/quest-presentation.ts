import type { QuestRecord, QuestProgressEvent } from "@/engine/contracts/gameplay";
import type { UiQuad } from "@/engine/contracts/ui";

// 5C4870/5C4980: objective state belongs to the contents node, never the
// parent quest's U10 (reward-window action). Keep both text children in sync.
// Do not extend the format grammar or interpret new fields without native
// evidence. No quest IDs or asset list here.
export function questObjectivePresentation(
	content: QuestRecord["contents"][number],
	entries: Readonly<Record<string, string>>
) {
	const format = entries[content.description] ?? "";
	let index = 0;
	const description = content.objectiveSentinel || content.objectiveValues.length === 0 ?
		format :
		format.replace(
			/%d/g,
			token => index < content.objectiveValues.length ? String( content.objectiveValues[index++]! | 0 ) : token
		);
	const complete = content.kind === 0 || content.kind === 2;
	const color: UiQuad["color"] = complete ? [ 1, 156 / 255, 104 / 255, 1 ] : [ 239 / 255, 218 / 255, 164 / 255, 1 ];
	return { description, statusKey: complete ? "UIIT_STT_QUEST_END" : "UIIT_STT_QUEST_ING", color };
}

// 5C4AD0 compares rendered strings, not raw counters or the parent U10.
export function questProgressText(
	event: QuestProgressEvent,
	entries: Readonly<Record<string, string>>
): string | null {
	const content = event.after;
	if ( content.kind === 0 ) return null;
	const count = content.objectiveSentinel ? 0 : content.objectiveValues.length;
	if ( count !== 1 && !content.description.startsWith( "CON_QTUTORIAL_EU" ) ) return null;
	const next = questObjectivePresentation( content, entries ).description;
	return next && next !== questObjectivePresentation( event.before, entries ).description ? next : null;
}

// CQuestData projection consumed by 5C3F90 and 5C1FA0. The quest text
// manager owns these symbols; textdataname is a different catalog.
// notify/condition/country are the projection the level-up QuestInfo
// window consumes (buildQuestDataAsset v4: source cols 9/10 and the
// countryBytes join); the reward window never reads them.
export interface QuestPresentation {
	readonly records: Readonly<
		Record<
			number,
			{
				readonly level: number;
				readonly title: string;
				readonly rewardTitle: string;
				readonly rewardBody: string;
				readonly warn: number;
				readonly progressClear: number;
				readonly notify: string;
				readonly condition: string;
				readonly country: number;
			}
		>
	>;
	readonly text: Readonly<Record<string, string>>;
}
export function decodeQuestPresentation( value: unknown ): QuestPresentation {
	const v = value as {
		version: number;
		rows: string[];
		giveupWarnBytes: number[];
		progressClearBytes: number[];
		countryBytes?: number[];
		textEntries: Record<string, string>;
	};
	if (
		v?.version !== 5 || !Array.isArray( v.rows ) || !Array.isArray( v.giveupWarnBytes ) ||
		v.rows.length !== v.giveupWarnBytes.length || !Array.isArray( v.progressClearBytes ) ||
		v.rows.length !== v.progressClearBytes.length ||
		v.progressClearBytes.some( n => !Number.isInteger( n ) || n < 0 || n > 255 ) ||
		(v.countryBytes !== undefined &&
			(!Array.isArray( v.countryBytes ) || v.rows.length !== v.countryBytes.length ||
				v.countryBytes.some( n => !Number.isInteger( n ) || n < 0 || n > 255 ))) ||
		!v.textEntries || typeof v.textEntries !== "object" || Array.isArray( v.textEntries ) ||
		Object.values( v.textEntries ).some( s => typeof s !== "string" )
	) throw Error( "Invalid quest presentation catalog" );
	const records: Record<number, QuestPresentation["records"][number]> = {};
	for ( const [i, line] of v.rows.entries() ) {
		if ( typeof line !== "string" ) throw Error( "Invalid quest presentation row" );
		const c = line.split( "\t" ), id = Number( c[0] ), level = Number( c[1] ), warn = v.giveupWarnBytes[i]!;
		if (
			c.length !== 7 || !Number.isSafeInteger( id ) || id <= 0 || !Number.isInteger( level ) || level < 0 ||
			level > 255 || !Number.isInteger( warn ) || warn < 0 || warn > 255
		) throw Error( "Invalid quest presentation fields" );
		if ( level > 90 || records[id] ) continue;
		records[id] = {
			level,
			title: v.textEntries[c[2]!.trim()] ?? "",
			rewardTitle: v.textEntries[c[3]!.trim()] ?? "",
			rewardBody: v.textEntries[c[4]!.trim()] ?? "",
			warn,
			progressClear: v.progressClearBytes[i]!,
			notify: v.textEntries[c[5]!.trim()] ?? "",
			condition: v.textEntries[c[6]!.trim()] ?? "",
			country: v.countryBytes?.[i] ?? 0
		};
	}
	return { records, text: { ...v.textEntries } };
}
