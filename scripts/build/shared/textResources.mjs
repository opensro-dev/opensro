// Split verbatim from resourcePipeline.mjs (2026-07-28): textdata cluster -
// text catalogs, worldmap/npcpos rows, message tips, event guide, mall notify.
import path from "node:path";

import { eventDataDir, publicRoot, readText, textDataDir, toGameRelative, writeJson } from "./resourceIo.mjs";
import { imagePublicPath } from "./cifResources.mjs";
import { iterateTextDataLines, readLocalizedTextDataRowsSync, stripFullLineComment } from "./textDataIo.mjs";
import { completeItemText, ITEM_TEXT_COMPLETIONS } from "./itemTextCompletions.mjs";
import { assertItemNameCoverage } from "./itemNameCoverage.mjs";
import { assertEnglishCompletionCoverage, completedEnglish, ENGLISH_COMPLETION_FILES } from "./englishCompletions.mjs";

export async function buildTextResources() {
	assertTextEnglishCoverage();
	const textCatalog = await buildTextCatalog( path.join( textDataDir, "textuisystem.txt" ) );
	completeUiText( textCatalog.entries );
	const zoneNameCatalog = await buildTextCatalog( path.join( textDataDir, "textzonename.txt" ) );
	// SN_* symbol -> text catalog (textdataname.txt): the world-map overlay
	// labels resolve their SN_ZONE_* keys through the localized-text manager
	// twin (sub_796330), seeded from this catalog.
	const dataNameCatalog = await buildTextCatalog( path.join( textDataDir, "textdataname.txt" ) );
	completeItemText( dataNameCatalog.entries );
	assertItemNameCoverage( textDataDir, dataNameCatalog.entries );
	// worldmap_localinfo.txt rows (native CGlobalDataManager_ParseScriptRecord
	// case 4): shipped as raw tab-token rows; the browser plane runs the REAL
	// sub_807cc0 parse fold per row into the 0xcec870 +0x1d8 record map twin.
	const worldMapLocalInfo = await buildWorldMapLocalInfoRows(
		path.join( textDataDir, "worldmap_localinfo.txt" )
	);
	// npcpos.txt rows (native media load @0x00723b34, parser sub_7f22d0 case
	// 0x1d -> the 0xcec870+0x414 CNPCPosData map): shipped as raw tab-token
	// rows; the quest plane runs the REAL sub_80d9d0 parse fold per row.
	const npcPosRows = await buildNpcPosRows( path.join( textDataDir, "npcpos.txt" ) );
	const regionCodeCatalog = await buildRegionCodeCatalog( path.join( textDataDir, "regioncode.txt" ) );
	const helpTextPath = path.join( textDataDir, "texthelp.txt" );
	const helpTextCatalog = await buildTextCatalog( helpTextPath );
	completeGuideTitles( helpTextCatalog.entries );
	const messageTipTextEntries = await buildStrictEnglishTextEntries( helpTextPath );
	const messageTipCatalog = await buildMessageTipCatalog(
		path.join( textDataDir, "messagetipdata.txt" ),
		helpTextCatalog,
		messageTipTextEntries
	);
	const eventGuideCatalog = await buildEventGuideCatalog(
		path.join( textDataDir, "gameguidedata.txt" ),
		helpTextCatalog,
		messageTipTextEntries
	);
	const mallNotifyData = await buildMallNotifyData( path.join( eventDataDir, "mall_notify.txt" ) );
	await writeJson( path.join( publicRoot, "assets", "text", "textuisystem.en.json" ), textCatalog );
	await writeJson( path.join( publicRoot, "assets", "text", "textzonename.en.json" ), zoneNameCatalog );
	await writeJson( path.join( publicRoot, "assets", "text", "textdataname.en.json" ), dataNameCatalog );
	await writeJson( path.join( publicRoot, "assets", "data", "worldmap-localinfo.json" ), worldMapLocalInfo );
	await writeJson( path.join( publicRoot, "assets", "data", "npcpos.json" ), npcPosRows );
	await writeJson( path.join( publicRoot, "assets", "text", "regioncode.json" ), regionCodeCatalog );
	await writeJson( path.join( publicRoot, "assets", "text", "texthelp.en.json" ), helpTextCatalog );
	await writeJson( path.join( publicRoot, "assets", "text", "messagetips.en.json" ), messageTipCatalog );
	await writeJson( path.join( publicRoot, "assets", "data", "event-guide-catalog.json" ), eventGuideCatalog );
	await writeJson( path.join( publicRoot, "assets", "data", "mall-notify.json" ), mallNotifyData );
	return {
		textCatalog,
		zoneNameCatalog,
		dataNameCatalog,
		worldMapLocalInfo,
		npcPosRows,
		regionCodeCatalog,
		helpTextCatalog,
		messageTipCatalog,
		eventGuideCatalog,
		mallNotifyData
	};
}
async function buildTextCatalog( sourcePath ) {
	const raw = await readText( sourcePath );
	const completionFile = englishCompletionFileOf( sourcePath );
	const entries = {};

	for ( const record of splitTextDataRecords( raw ) ) {
		if ( !record.trim() || record.trim().startsWith( "//" ) ) {
			continue;
		}

		const columns = record.split( "\t" );
		const key = columns[1]?.trim();
		if ( !key || key.startsWith( "//" ) ) {
			continue;
		}

		// Native 791F50 selects English column 8; 7933B0 preserves an empty cell.
		// Never turn translator notes/Korean into English UI text. Cells Joymax
		// left untranslated take the authored product completion
		// (englishCompletions.mjs); a shipped translation always overrides it.
		const english = completionFile ? completedEnglish( completionFile, columns ) : columns[8]?.trim() ?? "";
		entries[key] = english.replaceAll( "\\n", "\n" );
	}

	return {
		sourcePath: toGameRelative( sourcePath ),
		language: "en",
		entries
	};
}

// Product localization corrections, NOT byte-for-byte retail English parity.
// Keep the native capture/oracle unchanged when updating this override table.
// English completions for guide/quest titles Joymax shipped untranslated
// (empty or literal "0" English cells). These are translations of
// the row's own Korean label, corroborated by another shipped column or by
// Joymax-authored English zone/item text:
// - QCH/QWC/QKT/QTK regions: koreanLabels (Jangan/Donhwang/Hotan/Taklamakan)
//   match EN zone names (SN_ZONE_22003 Hotan, SN_ZONE_24010 Taklamakan,
//   Jangan/Donwhang fort zones) and the Chinese/Vietnamese columns
//   (Chang'an/Dunhuang/Hotan/Taklamakan readings).
// - QWC_DG/QCONS/QEASTEU/QAM/QCA/QSP groups: koreanLabels transliterate to
//   Donwhang Stone Cave (verbatim EN zone string), Constantinople (the
//   Europe town; children are Constantinople starter quests), East Europe,
//   Asia Minor, Central Asia, Special (trade-market children).
// - GDG guild menu/content: koreanLabels transliterate to Guild *; the
//   parent reading is attested by the shipped English child body
//   "[guild system]". Stub bodies echo their title plus the authored
//   revision ordinal, preserved here as " (1st)"/" (2nd)".
// Applied only when the built value has no ASCII letters, so a future
// Joymax translation automatically wins over these completions.
export const GUIDE_TITLE_COMPLETIONS = {
	SRO_GGW_MENU_QCH_MSG: "Jangan",
	SRO_GGW_MENU_QWC_MSG: "Donwhang",
	SRO_GGW_MENU_QKT_MSG: "Hotan",
	SRO_GGW_MENU_QTK_MSG: "Taklamakan",
	SRO_GGW_MENU_QWC_DG_MSG: "Donwhang Stone Cave",
	SRO_GGW_MENU_QCONS_MSG: "Constantinople",
	SRO_GGW_MENU_QEASTEU_MSG: "East Europe",
	SRO_GGW_MENU_QAM_MSG: "Asia Minor",
	SRO_GGW_MENU_QCA_MSG: "Central Asia",
	SRO_GGW_MENU_QSP_MSG: "Special",
	SRO_GGW_MENU_GDG_MSG: "Guild system",
	SRO_GGW_MENU_GDG_GROWTH2: "Guild growth",
	SRO_GGW_MENU_GDG_WAREHOUSE: "Guild warehouse",
	SRO_GGW_MENU_GDG_PAPER: "Guild/alliance note",
	SRO_GGW_MENU_GDG_SHOP: "Guild shop",
	SRO_GGW_MENU_GDG_CREST: "Guild/alliance crest",
	SRO_GGW_MENU_GDG_SOLDIER: "Guild mercenary",
	SRO_GGW_MENU_GDG_RECALL: "Guild recall",
	SRO_GGW_GDG_GROWTH2: "Guild growth (2nd)",
	SRO_GGW_GDG_WAREHOUSE: "Guild warehouse (1st)",
	SRO_GGW_GDG_PAPER: "Guild/alliance note (1st)",
	SRO_GGW_GDG_SHOP: "Guild shop (2nd)",
	SRO_GGW_GDG_CREST: "Guild/alliance crest (2nd)",
	SRO_GGW_GDG_SOLDIER: "Guild mercenary (2nd)",
	SRO_GGW_GDG_RECALL: "Guild recall (2nd)"
};
export function completeGuideTitles( entries ) {
	for ( const [key, english] of Object.entries( GUIDE_TITLE_COMPLETIONS ) ) {
		if ( key in entries && !/[A-Za-z]/.test( entries[key] ?? "" ) ) {
			entries[key] = english;
		}
	}
	return entries;
}

// Product localization disambiguations for regional NPC shop groups where official English
// omitted Chinese vs European distinctions present in Korean, Chinese, Japanese, and Vietnamese.
export const UI_TEXT_COMPLETIONS = Object.freeze( {
	SN_STORE_SMITH_GROUP1: "Purchase/sell/repair Chinese weapon",
	SN_STORE_SMITH_EU_GROUP1: "Purchase/sell/repair European weapon",
	SN_STORE_ARMOR_GROUP1: "Purchase/sell/repair Chinese protector for men",
	SN_STORE_ARMOR_GROUP2: "Purchase/sell/repair Chinese protector for women",
	SN_STORE_ARMOR_EU_GROUP1: "Purchase/sell/repair European protector for men",
	SN_STORE_ARMOR_EU_GROUP2: "Purchase/sell/repair European protector for women",
	SN_STORE_ACCESSORY_GROUP1: "Purchase/sell Chinese goods",
	SN_STORE_ACCESSORY_EU_GROUP1: "Purchase/sell European goods"
} );

// Product correction for defective shipped English, not a claim of native output
// for those inputs. 74BFF0 supplies six arguments: year/month/day/period/hour/minute.
// Preserve already-correct translations and retain the original typo key as data.
export function completeRestrictionText( entries ) {
	const chat = "UIIT_MSG_GM_PUNISHMENT_CHAT_BLOCK";
	const trade = "UIIT_MSG_GM_PUNISHMENT_TRADE_BLOCK";
	const malformedChat = "Chat restricted by GM. Can chat from %year %month %date %s %dhour %dminute(s).";
	const malformedTrade = "Trading restricted by GM. Can trade from %year %month %date %s %dhour %dminute(s).";
	if ( entries[chat] === malformedChat ) {
		entries[chat] = "Chat restricted by GM. Can chat from %d-%d-%d %s %dhour %dminute(s).";
	}
	if ( !(trade in entries) && entries["UIIT_MSG_GM_PUNISHMENT_TRADE _BLOCK"] === malformedTrade ) {
		entries[trade] = "Trading restricted by GM. Can trade from %d-%d-%d %s %dhour %dminute(s).";
	}
	return entries;
}

export function completeUiText( entries ) {
	completeRestrictionText( entries );
	// Client 7508C0 requests this key, but v1.150 omits the entire row.
	// Product completion uses the shipped Shaitan appearance/kill wording.
	if ( !entries.UIIT_MSG_DEAD_TAHOMET?.trim() ) {
		entries.UIIT_MSG_DEAD_TAHOMET = "Main shaitan has disappeared from Mt. Roc.";
	}
	for ( const [key, english] of Object.entries( UI_TEXT_COMPLETIONS ) ) {
		if ( key in entries ) {
			entries[key] = english;
		}
	}
	return entries;
}

/**
 * worldmap_localinfo.txt -> raw tab-token rows (comments dropped). The
 * columns mirror the native record-body token stream exactly:
 * id, kind, name x4, eight dwords (mode, pageId, regionX, regionY, posX,
 * posY, sizeW, sizeH/styleIndex), r, g, b, three state bytes - the browser
 * plane feeds them through the REAL sub_807cc0 parse fold.
 */
async function buildWorldMapLocalInfoRows( sourcePath ) {
	const raw = await readText( sourcePath );
	const rows = [];

	for ( const record of splitTextDataRecords( raw ) ) {
		const trimmed = record.trim();
		if ( !trimmed || trimmed.startsWith( "//" ) ) {
			continue;
		}
		const columns = record.split( "\t" ).map( ( column ) => column.trim() );
		if ( columns.length < 20 || !/^\d+$/.test( columns[0] ) ) {
			continue;
		}
		rows.push( columns.slice( 0, 20 ) );
	}

	return {
		format: "sro-worldmap-localinfo",
		version: 1,
		sourcePath: toGameRelative( sourcePath ),
		rows
	};
}

/*
 * npcpos.txt -> raw tab-token rows (comments dropped). The columns mirror
 * the native CNPCPosData record-body token stream exactly: id, regionId,
 * x, y, z (parser sub_80d9d0 @0x0080d9d0 - id/region via sub_9fbec0
 * ParseInt, x/y/z via sub_9fc210 ParseFloat). Native loads the file at
 * media boot ("%stextdata\\npcpos.txt" @0x00723b34, fmt data_c05184) and
 * the sub_7f22d0 case-0x1d walk (@0x007f481f) inserts each record into the
 * data_cec870+0x414 map - the browser quest plane (questPlane.ts) feeds
 * the SAME raw rows through the REAL parse folds into the map twin the
 * minimap quest pass resolves (REAL sub_7e1240).
 *
 * (Plain block comment, not JSDoc: the checkJs parser reads the @0x...
 * native offsets above as malformed JSDoc tags - TS1003.)
 */
async function buildNpcPosRows( sourcePath ) {
	const raw = await readText( sourcePath );
	const rows = [];

	for ( const rawLine of raw.split( /\r?\n/ ) ) {
		const line = rawLine.trim();
		if ( !line || line.startsWith( "//" ) ) {
			continue;
		}
		const columns = line.split( "\t" ).map( ( column ) => column.trim() );
		if ( columns.length < 5 || !/^\d+$/.test( columns[0] ) ) {
			continue;
		}
		// Ship the raw tab-joined row: the plane's tokenizer view runs the
		// REAL ScriptTokenizer folds over it, exactly like native's per-line
		// parse (no pre-parsing here - fidelity stays in the folds).
		rows.push( columns.slice( 0, 5 ).join( "\t" ) );
	}

	return {
		format: "sro-npcpos",
		version: 1,
		sourcePath: toGameRelative( sourcePath ),
		rows
	};
}

async function buildRegionCodeCatalog( sourcePath ) {
	const raw = await readText( sourcePath );
	const entries = {};
	const rows = [];

	for ( const line of iterateTextDataLines( raw, { trim: true } ) ) {
		const columns = line.split( "\t" ).map( ( column ) => column.trim() );
		const enabled = columns[0] === "1";
		const regionId = Number( columns[1] );
		const key = columns[2] ?? "";
		if (
			!enabled || !Number.isInteger( regionId ) || regionId < 0 || regionId > 0xffff ||
			!/^RN_[A-Z0-9_]+$/u.test( key )
		) {
			continue;
		}

		entries[String( regionId )] = key;
		rows.push( {
			regionId,
			key,
			label: columns[3] ?? ""
		} );
	}

	return {
		sourcePath: toGameRelative( sourcePath ),
		format: "sro-regioncode",
		version: 1,
		entries,
		rows
	};
}

async function buildStrictEnglishTextEntries( sourcePath ) {
	const raw = await readText( sourcePath );
	const entries = {};

	for ( const record of splitTextDataRecords( raw ) ) {
		if ( !record.trim() || record.trim().startsWith( "//" ) ) {
			continue;
		}

		const columns = record.split( "\t" );
		const key = columns[1]?.trim();
		if ( !key || key.startsWith( "//" ) ) {
			continue;
		}

		const completionFile = englishCompletionFileOf( sourcePath );
		const english = completionFile ? completedEnglish( completionFile, columns ) : columns[8]?.trim() ?? "";
		entries[key] = english.replaceAll( "\\n", "\n" );
	}

	return entries;
}

function englishCompletionFileOf( sourcePath ) {
	const name = path.basename( sourcePath ).toLowerCase();
	return ENGLISH_COMPLETION_FILES.includes( name ) ? name : null;
}

/**
 * Publication gate: every active row with Korean text has English, from
 * retail, an authored completion, or one of the key-specific layers.
 */
export function assertTextEnglishCoverage( textDataDirectory = textDataDir ) {
	const layers = new Set( [
		...Object.keys( ITEM_TEXT_COMPLETIONS ),
		...Object.keys( GUIDE_TITLE_COMPLETIONS ),
		...Object.keys( UI_TEXT_COMPLETIONS )
	] );
	for ( const file of ENGLISH_COMPLETION_FILES ) {
		const rows = readLocalizedTextDataRowsSync( path.join( textDataDirectory, file ) );
		assertEnglishCompletionCoverage( file, rows, ( key ) => layers.has( key ) );
	}
}

async function buildEventGuideCatalog( sourcePath, helpTextCatalog, strictEnglishEntries ) {
	const raw = await readText( sourcePath );
	const rows = [];
	const rowsById = {};
	const eventRowsByState = {};

	for ( const line of iterateTextDataLines( raw, { trim: true } ) ) {
		const columns = line.split( "\t" ).map( ( column ) => column.trim() );
		if ( columns.length < 7 ) {
			continue;
		}

		const enabled = columns[0] === "1";
		const id = Number( columns[1] );
		if ( !Number.isInteger( id ) ) {
			continue;
		}

		const depth = Number( columns[3] ) || 0;
		const parentOrCount = Number( columns[4] ) || 0;
		const contentKey = columns[5] || "";
		const menuKey = columns[6] || "";
		const europeanKey = id === 50001 ?
			"SRO_GGW_EVE_WELCOME_EUROPE" :
			id === 50003 ?
			"SRO_GGW_EVE_WEARARMOR_EUROPE" :
			id === 50013 ?
			"SRO_GGW_EVE_JOBCHOICE_EUROPE" :
			null;
		const row = {
			enabled,
			id,
			koreanLabel: columns[2] || "",
			depth,
			parentOrCount,
			contentKey,
			menuKey,
			englishContent: helpTextCatalog.entries[contentKey],
			// Never substitute translator notes (column 3) for missing English
			// content in the country-dependent native branches (667AB0).
			...(europeanKey ? { englishEuropeanContent: strictEnglishEntries[europeanKey] ?? "" } : {})
		};
		rows.push( row );
		rowsById[String( id )] = row;

		if ( id >= 50001 && id <= 50021 ) {
			eventRowsByState[String( id - 50000 )] = row;
		}
	}

	return {
		sourcePath: toGameRelative( sourcePath ),
		format: "sro-gameguidedata",
		version: 1,
		eventCategoryId: 50000,
		rows,
		rowsById,
		eventRowsByState
	};
}

async function buildMessageTipCatalog( sourcePath, helpTextCatalog, englishTextEntries ) {
	const raw = await readText( sourcePath );
	const rows = [];
	const rowsById = {};
	const rowsByTextKey = {};
	const unresolvedTextKeys = [];

	for ( const line of iterateTextDataLines( raw, { trim: true } ) ) {
		// Strict single-tab split preserving empty cells: the old \s+ split +
		// filter(Boolean) collapsed an empty field and mis-shaped the row. An
		// empty numeric cell parses as Number("") = 0 (the native atoi view).
		const columns = line.split( "\t" ).map( ( column ) => column.trim() );
		if ( columns.length < 7 || columns[0] !== "1" ) {
			continue;
		}

		const id = Number( columns[1] );
		const type = Number( columns[2] );
		const minLevel = Number( columns[3] );
		const maxLevel = Number( columns[4] );
		const group = Number( columns[5] );
		const textKey = columns[6] || "";
		if (
			!Number.isInteger( id ) ||
			!Number.isInteger( type ) ||
			!Number.isInteger( minLevel ) ||
			!Number.isInteger( maxLevel ) ||
			!Number.isInteger( group ) ||
			!textKey
		) {
			continue;
		}

		const text = englishTextEntries[textKey];
		if ( text === undefined ) {
			unresolvedTextKeys.push( textKey );
		}

		const row = {
			id,
			type,
			minLevel,
			maxLevel,
			group,
			textKey,
			text: text ?? ""
		};
		rows.push( row );
		rowsById[String( id )] = row;
		rowsByTextKey[textKey] = row;
	}

	return {
		sourcePath: toGameRelative( sourcePath ),
		textSourcePath: helpTextCatalog.sourcePath,
		format: "sro-messagetipdata",
		version: 1,
		language: "en",
		rows,
		rowsById,
		rowsByTextKey,
		unresolvedTextKeys
	};
}

async function buildMallNotifyData( sourcePath ) {
	const raw = await readText( sourcePath );
	const fields = {};

	for ( const rawLine of raw.split( /\r?\n/ ) ) {
		const parsed = parseQuotedKeyValue( rawLine );
		if ( !parsed ) {
			continue;
		}
		fields[parsed.key] = parsed.value;
	}

	return {
		sourcePath: toGameRelative( sourcePath ),
		format: "sro-mall-notify",
		version: 1,
		open: parseBooleanFlag( fields.Open, false ),
		textLineSpacing: parseInteger( fields.TextMargin, 5 ),
		textString: decodeRetailEscapes( fields.TextString ?? "" ),
		mallOpen: parseBooleanFlag( fields.Mall_Open, false ),
		mallFrontImage: fields.Mall_FrontImg ? imagePublicPath( fields.Mall_FrontImg ) : undefined,
		mallManItemString: fields.Mall_ManItemString ?? "",
		mallWomanItemString: fields.Mall_WomanItemString ?? "",
		mallSellingTime: fields.Mall_SellingTime ?? "",
		rawFields: fields
	};
}

function parseQuotedKeyValue( rawLine ) {
	const line = stripFullLineComment( rawLine ).trim();
	const match = /^([A-Za-z0-9_]+)\s*=\s*"([^"]*)"/.exec( line );
	if ( !match ) {
		return undefined;
	}

	return {
		key: match[1],
		value: match[2]
	};
}

function parseBooleanFlag( value, fallback ) {
	if ( value === undefined || value === null || value === "" ) {
		return fallback;
	}

	return value === "1" || /^true$/i.test( value );
}

function parseInteger( value, fallback ) {
	if ( value === undefined || value === null || value === "" ) {
		return fallback;
	}

	const parsed = Number.parseInt( value, 10 );
	return Number.isFinite( parsed ) ? parsed : fallback;
}

function decodeRetailEscapes( value ) {
	return String( value ?? "" )
		.replace( /\\[nN]/g, "\n" )
		.replace( /\\[tT]/g, "\t" );
}

// Exported for probe_parser_latent_textsplit.mjs, which proves the
// record-start rule against the real splitter rather than a replica.
export function splitTextDataRecords( raw ) {
	const records = [];
	let currentRecord = "";

	for ( const line of raw.split( /\r?\n/ ) ) {
		const trimmed = line.trim();

		if ( trimmed.startsWith( "//" ) ) {
			if ( currentRecord ) {
				records.push( currentRecord );
				currentRecord = "";
			}
			continue;
		}

		// Record-start rule: a leading "<digits>\t" starts a record even when the
		// SECOND field is empty ("123\t\tX...") - the old /^\d+\t[^\t]+\t/ demanded
		// a non-empty second field, so such a line silently merged into the
		// previous record as a fake continuation, corrupting its columns. Scope:
		// this splitter serves exactly the five files fed through it (textuisystem,
		// textzonename, textdataname, texthelp, worldmap_localinfo), where
		// continuation lines never begin with "<digits>\t"; other textdata files
		// with 2-column rows (erasableskill.txt etc.) never flow through here, so
		// the relaxed rule must not be copied to their parsers.
		if ( /^\d+\t/.test( line ) ) {
			if ( currentRecord ) {
				records.push( currentRecord );
			}
			currentRecord = line;
			continue;
		}

		if ( currentRecord ) {
			currentRecord += `\n${line}`;
		}
	}

	if ( currentRecord ) {
		records.push( currentRecord );
	}

	return records;
}
