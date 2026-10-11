/*
===========================================================================

buildCatalogue.mjs - the website's static game catalogue (opensro-web C1-C9)

Two stages. The Go tool sro-catalogue writes what the server knows (the
reference rows, statuses, odds) and the JSON Schemas; this stage adds what
only the client knows: each item's tooltip, built by client-next's own
itemTooltip, and the web icons, encoded to WebP from the client's published
PNGs. Output: <generated>/catalogue/<release>/ and catalogue/latest.json.

usage: node scripts/build/catalogue/buildCatalogue.mjs [--release <id>] [--out <dir>] [--since <commit>]
       [--portraits <viewer base url>]
(--out writes the catalogue somewhere else, for a trial run; the inputs
always come from the generated tree. --since is the previous release's
commit: notes.json lists the player notes of the merges after it.
--portraits draws each monster's portrait through the client's viewer,
which must already be served at that URL, e.g. http://127.0.0.1:5173/.)

===========================================================================
*/
// The client's own TypeScript loader (resolve and transform-mode type
// stripping), the one its tests and tools register.
import "../../../apps/client-next/tests/helpers/native-source-loader.mjs";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { clientPublicPath, generatedPath } from "../../lib/generatedRoot.mjs";
import { runPython } from "../shared/pythonRun.mjs";
import { playerNotes } from "./playerNotes.mjs";
import { renderPortraits } from "./renderPortraits.mjs";

const { itemTooltip } = await import( "../../../apps/client-next/src/engine/foundation/ui/item-tooltip.ts" );
const { itemMaxDurability } = await import(
	"../../../apps/client-next/src/engine/foundation/ui/item-tooltip-stats.ts"
);
const { itemTooltipReference } = await import(
	"../../../apps/client-next/src/engine/foundation/gameplay/item-tooltip-reference.ts"
);
const { skillTooltip } = await import( "../../../apps/client-next/src/engine/foundation/ui/skill-tooltip.ts" );
const { createTooltipSkillDecoder } = await import(
	"../../../apps/client-next/src/engine/foundation/ui/skill-tooltip-catalog.ts"
);

const repositoryRoot = fileURLToPath( new URL( "../../../", import.meta.url ) );
const serverRoot = path.join( repositoryRoot, "apps", "server" );
// The bag's first slot: the tooltip reads an item instance, and a catalogue
// row is the item as it drops (no plus, no options, lowest variance).
const CATALOGUE_SLOT = 13;
// A requirement value no item asks for: requirements read as met (white),
// since the catalogue has no player to compare against.
const MET = 0xffff;

/*
================
parseArguments
================
*/
function parseArguments( argv ) {
	const options = { release: "dev", out: "", since: "", portraits: "" };
	for ( let i = 0; i < argv.length; i++ ) {
		if ( argv[i] === "--release" ) options.release = argv[++i] ?? "";
		else if ( argv[i] === "--out" ) options.out = path.resolve( argv[++i] ?? "" );
		else if ( argv[i] === "--since" ) options.since = argv[++i] ?? "";
		else if ( argv[i] === "--portraits" ) options.portraits = argv[++i] ?? "";
		else throw Error( "unknown argument " + argv[i] );
	}
	if ( !/^[A-Za-z0-9._-]{1,64}$/.test( options.release ) ) throw Error( "invalid --release " + options.release );
	if ( options.since && !/^[0-9a-f]{7,40}$/i.test( options.since ) ) {
		throw Error( "invalid --since " + options.since );
	}
	if ( options.portraits && !/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\/?$/.test( options.portraits ) ) {
		throw Error( "--portraits must be the loopback viewer's base URL, e.g. http://127.0.0.1:5173/" );
	}
	return options;
}

/*
================
readJSON
================
*/
function readJSON( file ) {
	return JSON.parse( readFileSync( file, "utf8" ) );
}

/*
================
textLookup

The client's tooltip lookup: the display-name catalogue first, then the
system text (ui.ts lookup and hudCopy).
================
*/
function textLookup() {
	const catalogues = [ "textdataname.en.json", "textuisystem.en.json", "texthelp.en.json" ].map( name =>
		readJSON( clientPublicPath( "assets", "text", name ) ).entries ?? {}
	);
	return symbol => {
		if ( !symbol || symbol === "xxx" ) return "";
		for ( const entries of catalogues ) {
			if ( typeof entries[symbol] === "string" && entries[symbol] ) return entries[symbol];
		}
		return "";
	};
}

/*
================
tooltipRows

One catalogue item's tooltip, from the client's own builder over the item
as it drops, with requirements shown as met.
================
*/
function tooltipRows( reference, text ) {
	const tooltip = itemTooltipReference( reference.nativeFields, reference.descriptionSymbol );
	const base = {
		slot: CATALOGUE_SLOT,
		refObjId: reference.refObjId,
		typeFlags: reference.typeFlags,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		name: reference.name,
		tooltip
	};
	const item = { ...base, durability: itemMaxDurability( base ) };
	const masteries = [];
	for ( let i = 1; i <= 4; i++ ) {
		const type = tooltip?.fields["reqLevelType" + i];
		if ( type !== undefined && type > 10 ) masteries.push( { id: type, level: MET } );
	}
	const progression = { level: MET, stats: { strength: MET, intellect: MET }, masteries };
	// The race and sex rows read as met too: the wearer is whoever may wear it.
	const country = tooltip?.fields.country, sex = tooltip?.fields.reqGender;
	const identity = {
		...(country === 0 || country === 1 ? { country } : {}),
		...(sex === 0 || sex === 1 ? { sex } : {})
	};
	return itemTooltip( item, progression, text, identity ).map( row =>
		row.value.trim() === "" ?
			{ text: "" } :
			{ text: row.value, color: "#" + (row.color & 0xffffff).toString( 16 ).padStart( 6, "0" ) }
	);
}

/*
================
hexRows

Client tooltip rows as catalogue rows: text and #rrggbb, a blank line bare.
================
*/
function hexRows( rows ) {
	return rows.map( row =>
		row.value.trim() === "" ?
			{ text: "" } :
			{ text: row.value, color: "#" + (row.color & 0xffffff).toString( 16 ).padStart( 6, "0" ) }
	);
}

/*
================
fillSkills

Each skill's name, mastery, icon and tooltip from the client's skill window
data (skillUi.json) and its own tooltip builder over skillData.json, as a
character who meets every requirement and has learned nothing sees it. The
mastery list comes from the same window data; race is the icon's folder.
================
*/
function fillSkills( skills, text ) {
	const ui = readJSON( clientPublicPath( "assets", "data", "skillUi.json" ) );
	const catalog = createTooltipSkillDecoder( readJSON( clientPublicPath( "assets", "data", "skillData.json" ) ) )
		.step(
			Number.MAX_SAFE_INTEGER
		);
	const masteryName = id => text( ui.masteries.find( m => m.id === id )?.name ?? "" );
	skills.masteries = ui.masteries.map( m => {
		const icon = webIconPath( m.icon );
		return {
			id: m.id,
			name: text( m.name ) || String( m.id ),
			race: /\/europe\//.test( icon ) ? "european" : "chinese",
			icon
		};
	} );
	const byId = new Map( ui.skills.map( s => [ s.id, s ] ) );
	const ranks = new Map( skills.skills.map( s => [ s.group + ":" + s.level, s.id ] ) );
	// Each group's highest rank: a requirement is "at least", so it is always met.
	const top = new Map();
	for ( const s of [ ...skills.skills ].sort( ( a, b ) => a.level - b.level ) ) top.set( s.group, s.id );
	const progression = {
		level: MET,
		skillPoints: Number.MAX_SAFE_INTEGER,
		stats: { strength: MET, intellect: MET },
		masteries: ui.masteries.map( m => ({ id: m.id, level: MET }) )
	};
	for ( const skill of skills.skills ) {
		const row = byId.get( skill.id );
		if ( row ) {
			skill.name = text( row.name ) || skill.name;
			skill.mastery = row.mastery;
			skill.icon = webIconPath( row.icon );
		}
		skill.tooltip = hexRows(
			skillTooltip(
				{ id: skill.id, catalog, learned: learnedFor( skill, ranks, top ), progression },
				text,
				masteryName
			)
		);
		// A rank with no name text (a weapon's basic attack, a hidden buff)
		// heads its tooltip with only " Lv n": give it the catalogue name.
		const heading = skill.tooltip[0];
		if ( heading?.text.startsWith( " Lv" ) ) heading.text = skill.name + heading.text;
	}
}

/*
================
learnedFor

The skills a reader of this rank is taken to have learned: every required
skill group at its highest rank, and the rank before this one, so the
tooltip shows this rank's own requirements, all met.
================
*/
function learnedFor( skill, ranks, top ) {
	const learned = skill.prerequisites.map( p => top.get( p.id ) ).filter( id => id !== undefined );
	const previous = ranks.get( skill.group + ":" + (skill.level - 1) );
	if ( previous !== undefined ) learned.push( previous );
	return learned;
}

/*
================
webIconPath

A client icon path as the catalogue's icons/ path (as the Go webIcon).
Skill-window paths carry the icon folder itself ("icon/skill/...").
================
*/
function webIconPath( media ) {
	const p = (media ?? "").trim().replaceAll( "\\", "/" ).toLowerCase();
	return !p || p === "xxx" ? "" : p.replace( /^icon\//, "" ).replace( /\.ddj$/, "" ) + ".webp";
}

/*
================
fillRegions

Every region the client names: its area and place from regioncode (the
region code RN_<AREA>_<PLACE>) and its English name from textzonename,
both keyed by region id.
================
*/
function fillRegions( regions ) {
	const codes = readJSON( clientPublicPath( "assets", "text", "regioncode.json" ) ).entries ?? {};
	const names = readJSON( clientPublicPath( "assets", "text", "textzonename.en.json" ) ).entries ?? {};
	const ids = new Set( [ ...Object.keys( codes ), ...Object.keys( names ) ].map( Number ) );
	regions.regions = [ ...ids ].filter( id => id > 0 ).sort( ( a, b ) => a - b ).map( id => {
		const place = codes[id] ?? "", area = /^RN_([A-Z]+)_/.exec( place )?.[1]?.toLowerCase();
		return {
			id,
			...(area ? { area } : {}),
			...(place ? { place } : {}),
			...(names[id] ? { name: names[id] } : {})
		};
	} );
}

/*
================
searchEntries

The name index: every item, quest, monster and NPC, each skill group once
(its first rank), and every named region once per name.
================
*/
function searchEntries( files ) {
	const entries = [];
	const add = ( kind, rows ) => {
		for ( const row of rows ) {
			if ( row.name ) {
				entries.push( { kind, id: row.id, name: row.name, ...(row.icon ? { icon: row.icon } : {}) } );
			}
		}
	};
	add( "item", files.items );
	const groups = new Map();
	for ( const skill of files.skills ) {
		const first = groups.get( skill.group );
		if ( !first || skill.level < first.level ) groups.set( skill.group, skill );
	}
	add( "skill", [ ...groups.values() ] );
	add( "quest", files.quests );
	add( "monster", files.monsters );
	add( "npc", files.npcs );
	const names = new Map();
	for ( const region of files.regions ) {
		if ( region.name && !names.has( region.name ) ) names.set( region.name, region );
	}
	add( "region", [ ...names.values() ] );
	return entries;
}

/*
================
atlasTiles

Every world region's minimap art the client publishes (minimap/<x>x<z>,
a .texture or a .png) and the WebP it becomes (atlas/<x>x<z>.webp), in
name order.
================
*/
function atlasTiles( out ) {
	const directory = clientPublicPath( "assets", "images", "Media_extracted", "minimap" );
	const tiles = [];
	for ( const name of readdirSync( directory ).sort() ) {
		const match = /^(\d+x\d+)\.(?:texture|png)$/i.exec( name );
		if ( match ) {
			tiles.push( [ match[1], [ path.join( directory, name ), path.join( out, "atlas", match[1] + ".webp" ) ] ] );
		}
	}
	return tiles;
}

/*
================
iconPairs

The PNG the client publishes for each icon and the WebP the catalogue
writes beside the JSON (icons/<path>.webp).
================
*/
function iconPairs( rows, out ) {
	const pairs = new Map();
	for ( const item of rows ) {
		if ( !item.icon || pairs.has( item.icon ) ) continue;
		const source = clientPublicPath(
			"assets",
			"images",
			"Media_extracted",
			"icon",
			...item.icon.replace( /\.webp$/, ".png" ).split( "/" )
		);
		if ( existsSync( source ) ) {
			pairs.set( item.icon, [ source, path.join( out, "icons", ...item.icon.split( "/" ) ) ] );
		}
	}
	return [ ...pairs.entries() ];
}

/*
================
main
================
*/
async function main() {
	const options = parseArguments( process.argv.slice( 2 ) );
	const root = options.out || generatedPath( "catalogue" ), out = path.join( root, options.release );
	const work = mkdtempSync( path.join( tmpdir(), "sro-catalogue-" ) );
	try {
		execFileSync( "go", [ "run", "./cmd/tools/sro-catalogue", "-out", work, "-release", options.release ], {
			cwd: serverRoot,
			stdio: [ "ignore", "inherit", "inherit" ]
		} );
		const text = textLookup();
		const references = new Map(
			readJSON( path.join( work, "_build", "item-references.json" ) ).map( r => [ r.refObjId, r ] )
		);
		const items = readJSON( path.join( work, "items.json" ) );
		for ( const item of items.items ) {
			const reference = references.get( item.id );
			if ( reference ) item.tooltip = tooltipRows( reference, text );
		}
		const skills = readJSON( path.join( work, "skills.json" ) );
		fillSkills( skills, text );
		const iconRows = [ ...items.items, ...skills.skills, ...skills.masteries ];
		const pairs = iconPairs( iconRows, out );
		const iconSet = new Set( pairs.map( ( [icon] ) => icon ) );
		for ( const row of iconRows ) {
			if ( row.icon && !iconSet.has( row.icon ) ) delete row.icon;
		}
		rmSync( out, { recursive: true, force: true } );
		mkdirSync( path.join( out, "schema" ), { recursive: true } );
		writeFileSync( path.join( out, "items.json" ), JSON.stringify( items ) + "\n" );
		writeFileSync( path.join( out, "skills.json" ), JSON.stringify( skills ) + "\n" );
		const regions = readJSON( path.join( work, "regions.json" ) );
		fillRegions( regions );
		writeFileSync( path.join( out, "regions.json" ), JSON.stringify( regions ) + "\n" );
		const npcs = readJSON( path.join( work, "npcs.json" ) );
		for ( const tab of npcs.npcs.flatMap( npc => npc.tabs ) ) {
			tab.label = text( tab.labelSymbol ) || tab.labelSymbol;
		}
		writeFileSync( path.join( out, "npcs.json" ), JSON.stringify( npcs ) + "\n" );
		const search = readJSON( path.join( work, "search.json" ) );
		search.entries = searchEntries( {
			items: items.items,
			skills: skills.skills,
			quests: readJSON( path.join( work, "quests.json" ) ).quests,
			monsters: readJSON( path.join( work, "monsters.json" ) ).monsters,
			npcs: npcs.npcs,
			regions: regions.regions
		} );
		writeFileSync( path.join( out, "search.json" ), JSON.stringify( search ) + "\n" );
		const notes = readJSON( path.join( work, "notes.json" ) );
		notes.since = options.since;
		notes.entries = playerNotes( repositoryRoot, options.since );
		writeFileSync( path.join( out, "notes.json" ), JSON.stringify( notes ) + "\n" );
		const monsters = readJSON( path.join( work, "monsters.json" ) );
		const portraitPairs = [];
		if ( options.portraits ) {
			const { drawn, failed } = await renderPortraits( {
				viewerBase: options.portraits,
				monsters: monsters.monsters,
				workDir: path.join( work, "portraits" )
			} );
			for ( const monster of monsters.monsters ) {
				const png = drawn.get( monster.id );
				if ( !png ) continue;
				monster.portrait = "portraits/" + monster.id + ".webp";
				portraitPairs.push( [ png, path.join( out, "portraits", monster.id + ".webp" ) ] );
			}
			for ( const failure of failed ) {
				console.log(
					`portrait ${failure.id}: ${failure.reason}${failure.stage ? " at " + failure.stage : ""}`
				);
			}
		}
		writeFileSync( path.join( out, "monsters.json" ), JSON.stringify( monsters ) + "\n" );
		const atlas = readJSON( path.join( work, "atlas.json" ) );
		const tiles = atlasTiles( out );
		atlas.tiles = tiles.map( ( [key] ) => key.split( "x" ).map( Number ) );
		writeFileSync( path.join( out, "atlas.json" ), JSON.stringify( atlas ) + "\n" );
		// Every other file and schema the Go tool wrote is published as written.
		for ( const name of readdirSync( work ) ) {
			if (
				name.endsWith( ".json" ) &&
				![
					"items.json",
					"skills.json",
					"regions.json",
					"npcs.json",
					"search.json",
					"atlas.json",
					"notes.json",
					"monsters.json"
				]
					.includes(
						name
					)
			) {
				writeFileSync( path.join( out, name ), readFileSync( path.join( work, name ) ) );
			}
		}
		for ( const schema of readdirSync( path.join( work, "schema" ) ) ) {
			writeFileSync( path.join( out, "schema", schema ), readFileSync( path.join( work, "schema", schema ) ) );
		}
		const list = path.join( work, "icons.json" );
		writeFileSync( list, JSON.stringify( pairs.map( ( [, pair] ) => pair ) ) );
		await runPython( [ fileURLToPath( new URL( "./encode_icons.py", import.meta.url ) ), list ], {
			task: "catalogue icons"
		} );
		const tileList = path.join( work, "tiles.json" );
		writeFileSync( tileList, JSON.stringify( tiles.map( ( [, pair] ) => pair ) ) );
		await runPython( [ fileURLToPath( new URL( "./encode_tiles.py", import.meta.url ) ), tileList ], {
			task: "catalogue atlas tiles"
		} );
		if ( portraitPairs.length ) {
			const portraitList = path.join( work, "portraits.json" );
			writeFileSync( portraitList, JSON.stringify( portraitPairs ) );
			await runPython( [ fileURLToPath( new URL( "./encode_icons.py", import.meta.url ) ), portraitList ], {
				task: "catalogue portraits"
			} );
		}
		writeFileSync(
			path.join( root, "latest.json" ),
			JSON.stringify( { release: options.release, generatedAt: items.generatedAt } ) + "\n"
		);
		console.log(
			`catalogue ${options.release}: ${items.items.length} items, ${pairs.length} icons, ${tiles.length} atlas tiles -> ${out}`
		);
	} finally {
		rmSync( work, { recursive: true, force: true } );
	}
}

await main();
