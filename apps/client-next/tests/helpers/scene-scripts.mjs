/*
===========================================================================

scene-scripts.mjs - the recorded event scripts of the scene goldens

One script per window (#2514): a fixed sequence of state changes and UI
events, driven against the production HUD through tests/helpers'
uiFixture at a fixed clock, whose settled published scene (and, for the
gesture tapes, the published stream between beats) must stay
byte-identical across the ui.ts split. A script moves nothing; it only
records. Recipes are the openings the behavioural suites already prove
(open-window:<Panel>, the panel sweep's service openers, the npc talk
flow), with the smallest real content that makes the window draw its
own rows - an empty frame alone would not notice a one-pixel move in
the rows a move actually touches.

===========================================================================
*/

/*
================
item

One inventory row in the shape the worker publishes.
================
*/
function item( slot, name, icon, quantity = 1 ) {
	return { slot, refObjId: 1, typeFlags: (3 << 2) | (3 << 5) | (13 << 7) | (9 << 11), quantity, name, icon };
}

const POTION = () => item( 13, "HP potion", "item/etc/hp_potion_01.ddj", 20 );
const ELIXIR = () => item( 14, "Elixir", "item/etc/archemy_reinforce_recipe_weapon_a.ddj" );

/*
================
guildTalk

Face the guild manager with a small guild behind us: the conversation
the manager's ask, field and vote dialogs all hang off.
================
*/
function guildTalk( ctx ) {
	const member = ( id, name, grade ) => ({
		id,
		name,
		grade,
		level: 1,
		donated: 0,
		permissions: 0,
		grant: "",
		model: 0,
		role: 0,
		offline: 0
	});
	ctx.state.entities.push( {
		...ctx.state.entities[0],
		gid: 17,
		refObjId: 2011,
		kind: "npc",
		name: "Guild Manager"
	} );
	Object.assign( ctx.state.gameplay, {
		target: 17,
		targetCapabilities: 0x4000,
		npcConversation: { phase: "menu", gid: 17 },
		social: {
			invitation: null,
			guild: {
				id: 9,
				name: "Lanterns",
				level: 2,
				gp: 0,
				subject: "",
				contents: "",
				members: [ member( 1, "Master", 0 ), member( 2, "Member", 10 ) ]
			}
		}
	} );
}

/*
================
beat

One labelled beat of a script: an optional state change, then events.
ticks overrides how many 100 ms steps the world settles over afterwards
(a dialog on a native timer, like the rebirth prompt, needs its full
3000 ms before it publishes).
================
*/
function beat( label, run, ticks ) {
	return { label, run, ticks };
}

/*
================
openPanel

The one beat most windows are: open by window id and settle.
================
*/
function openPanel( panel, patch ) {
	return [ beat( "open", ctx => {
		patch?.( ctx );
		ctx.event( { kind: "activate", id: "open-window:" + panel } );
	} ) ];
}

/*
================
SCENE_SCRIPTS

name: the golden's file name. stream: snapshot after every beat (the
gesture tapes). guard: the settled controls must differ from the plain
world HUD, so a recipe that stops opening its window fails loudly
instead of blessing an empty frame.

===========================================================================
*/
export const SCENE_SCRIPTS = [
	// --- the frontend screens (state-driven, no world) -------------------
	{
		name: "frontend-title",
		guard: true,
		beats: [ beat( "title", ctx => {
			ctx.state.session = { phase: "disconnected", revision: 1 };
			ctx.state.frontend = { phase: "login", generation: 1, elapsed: 10, alpha: 1, logoAlpha: 0, error: null };
			ctx.state.worldReady = false;
		} ) ]
	},
	{
		name: "frontend-character-select",
		guard: true,
		beats: [ beat( "select", ctx => {
			ctx.state.session = {
				phase: "character-select",
				revision: 4,
				characters: [ {
					name: "Player",
					level: 20,
					regionId: 1,
					x: 0,
					y: 0,
					z: 0,
					heading: 0,
					kind: "player",
					mountedOn: 0,
					gid: 1
				} ]
			};
			ctx.state.frontend = {
				phase: "login-accepted",
				generation: 1,
				elapsed: 10,
				alpha: 1,
				logoAlpha: 0,
				error: null
			};
			ctx.state.worldReady = false;
		} ) ]
	},
	{
		name: "loading-world",
		guard: true,
		beats: [ beat( "loading", ctx => {
			ctx.state.frontend = { phase: "loading-world", generation: 1 };
			ctx.state.worldReady = false;
		} ) ]
	},

	// --- the world HUD ----------------------------------------------------
	{ name: "hud-world", beats: [ beat( "settle", () => {} ) ] },
	{
		name: "world-overheads",
		beats: [ beat( "settle", ctx => {
			ctx.state.entities.push(
				{
					gid: 2,
					regionId: 1,
					x: 30,
					y: 0,
					z: 40,
					heading: 0,
					kind: "monster",
					name: "Mangyang",
					mountedOn: 0
				},
				{
					gid: 3,
					regionId: 1,
					x: -25,
					y: 0,
					z: 55,
					heading: 0,
					kind: "player",
					name: "Neighbour",
					mountedOn: 0
				}
			);
		} ) ]
	},

	// --- the panels ---------------------------------------------------------
	{
		name: "panel-inventory",
		guard: true,
		beats: openPanel( "Inventory", ctx => {
			ctx.state.gameplay.inventory = [ POTION(), ELIXIR() ];
		} )
	},
	{ name: "panel-character", guard: true, beats: openPanel( "Character" ) },
	{ name: "panel-skills", guard: true, beats: openPanel( "Skills" ) },
	{ name: "panel-actions", guard: true, beats: openPanel( "Actions" ) },
	{ name: "panel-quests", guard: true, beats: openPanel( "Quests" ) },
	{ name: "panel-option", guard: true, beats: openPanel( "Option" ) },
	{ name: "panel-system", guard: true, beats: openPanel( "System" ) },
	{ name: "panel-chat", guard: true, beats: openPanel( "Chat" ) },
	{ name: "panel-blocking", guard: true, beats: openPanel( "Blocking" ) },
	{ name: "panel-auto-potion", guard: true, beats: openPanel( "Auto Potion" ) },
	{ name: "panel-experimental", guard: true, beats: openPanel( "Experimental" ) },
	{ name: "panel-game-guide", guard: true, beats: openPanel( "Game Guide" ) },
	{ name: "panel-map", guard: true, beats: openPanel( "Map" ) },
	{ name: "panel-magic-pang", guard: true, beats: openPanel( "Magic Pang" ) },
	{ name: "panel-party", guard: true, beats: openPanel( "Party" ) },
	{ name: "panel-party-matching", guard: true, beats: openPanel( "Party Matching" ) },
	{ name: "panel-guild", guard: true, beats: openPanel( "Guild" ) },
	{ name: "panel-guild-tools", guard: true, beats: openPanel( "Guild tools" ) },
	{ name: "panel-academy", guard: true, beats: openPanel( "Academy" ) },
	{
		name: "panel-alchemy",
		guard: true,
		beats: openPanel( "Alchemy", ctx => {
			ctx.state.gameplay.inventory = [ ELIXIR() ];
		} )
	},
	{
		name: "panel-cos-inventory",
		guard: true,
		beats: openPanel( "COS inventory", ctx => {
			ctx.state.gameplay.cosRecords = [ {
				gid: 7,
				refObjId: 100,
				band: 4,
				hp: 100,
				mp: 0,
				status: 0,
				dead: false
			} ];
		} )
	},
	{
		name: "panel-storage",
		guard: true,
		beats: openPanel( "Storage", ctx => {
			ctx.state.gameplay = {
				...ctx.state.gameplay,
				storage: { npc: 17, phase: "open", capacity: 150, gold: "0", items: [ POTION() ] }
			};
		} )
	},
	{
		name: "panel-academy-matching",
		guard: true,
		beats: openPanel( "Academy Matching", ctx => {
			ctx.state.gameplay.academy = { member: false, rows: [], request: null, page: 0 };
		} )
	},
	{
		name: "panel-shop",
		guard: true,
		beats: [ beat( "open", ctx => {
			ctx.state.entities.push( {
				...ctx.state.entities[0],
				gid: 18,
				refObjId: 2011,
				kind: "npc",
				name: "Trader"
			} );
			Object.assign( ctx.state.gameplay, {
				target: 18,
				shop: { npc: 18, name: "Trader", offers: [], cosGid: 0 }
			} );
			ctx.event( { kind: "activate", id: "open-window:Shop" } );
		} ) ]
	},

	// --- the item mall ------------------------------------------------------
	{
		name: "item-mall",
		guard: true,
		beats: [ beat( "open", ctx => ctx.event( { kind: "activate", id: "item-mall" } ) ) ]
	},

	// --- the dialogs ----------------------------------------------------------
	{
		name: "dialog-npc-talk",
		guard: true,
		beats: [ beat( "talk", ctx => {
			ctx.state.entities.push( {
				...ctx.state.entities[0],
				gid: 17,
				refObjId: 2011,
				kind: "npc",
				name: "Official"
			} );
			Object.assign( ctx.state.gameplay, { target: 17, npcConversation: { phase: "menu", gid: 17 } } );
		} ) ]
	},
	{
		name: "dialog-fortress-war",
		guard: true,
		beats: [
			beat( "talk", ctx => {
				ctx.state.entities.push( {
					...ctx.state.entities[0],
					gid: 17,
					refObjId: 2011,
					kind: "npc",
					name: "Official"
				} );
				Object.assign( ctx.state.gameplay, { target: 17, npcConversation: { phase: "menu", gid: 17 } } );
			} ),
			beat( "war", ctx => {
				ctx.event( { kind: "activate", id: "npc-fortress-war" } );
				ctx.state.gameplay.fortressApplication = { warStart: null, applied: null, sequence: 1 };
			} )
		]
	},
	{
		name: "dialog-party-settings",
		guard: true,
		beats: [
			...openPanel( "Party" ),
			beat( "settings", ctx => ctx.event( { kind: "activate", id: "party-settings" } ) )
		]
	},
	{
		name: "dialog-rebirth",
		guard: true,
		beats: [
			// The death state enters in two steps: LIFE=dead arrives with
			// the windup, the fatal impact (zero HP, death flag) lands
			// after; the prompt follows on its native 3000 ms timer, so
			// the impact beat settles a full 6400 ms. State lands as
			// fresh objects, the way the worker publishes a snapshot - a
			// field mutated in place is never re-ingested.
			beat( "windup", ctx => {
				ctx.state.gameplay = {
					...ctx.state.gameplay,
					progression: { level: 10, masteries: [] },
					vitals: [ { gid: 1, hp: 0, mp: 0 } ]
				};
				ctx.state.entities = [ { ...ctx.state.entities[0], appearanceState: [ 2, 0, 0 ] } ];
			} ),
			beat( "impact", ctx => {
				ctx.state.gameplay = {
					...ctx.state.gameplay,
					vitals: [ { gid: 1, hp: 0, mp: 0, deathState: true } ]
				};
			}, 64 )
		]
	},
	{
		name: "dialog-resurrection",
		guard: true,
		beats: [
			beat( "windup", ctx => {
				ctx.state.gameplay = {
					...ctx.state.gameplay,
					progression: { level: 10, masteries: [] },
					vitals: [ { gid: 1, hp: 0, mp: 0 } ]
				};
				ctx.state.entities = [ { ...ctx.state.entities[0], appearanceState: [ 2, 0, 0 ] } ];
			} ),
			beat( "impact", ctx => {
				ctx.state.gameplay = {
					...ctx.state.gameplay,
					vitals: [ { gid: 1, hp: 0, mp: 0, deathState: true } ]
				};
			}, 64 ),
			beat( "offer", ctx => {
				ctx.state.gameplay = {
					...ctx.state.gameplay,
					social: { invitation: null, resurrection: { gid: 2 } }
				};
			} )
		]
	},
	{
		name: "dialog-notice",
		guard: true,
		beats: [ beat( "notice", ctx => {
			ctx.state.gameplay.notices = [ {
				sequence: 1,
				key: "",
				value: 0,
				bannerOnly: true,
				dialog: {
					title: "UIIT_STT_EVENTGUIDE",
					lines: [ "UIIT_MSG_GUILDWAR_SUGGESTIONS_01", "UIIT_MSG_GUILDWAR_SUGGESTIONS_02" ]
				}
			} ];
		} ) ]
	},
	{
		name: "dialog-guild-ask",
		guard: true,
		beats: [
			beat( "talk", ctx => guildTalk( ctx ) ),
			beat( "ask", ctx => ctx.event( { kind: "activate", id: "npc-guild:level-up" } ) )
		]
	},
	{
		name: "dialog-guild-field",
		guard: true,
		beats: [
			beat( "talk", ctx => guildTalk( ctx ) ),
			beat( "field", ctx => ctx.event( { kind: "activate", id: "npc-guild:create" } ) )
		]
	},
	{
		name: "dialog-guild-vote",
		guard: true,
		beats: [
			beat( "talk", ctx => {
				guildTalk( ctx );
				ctx.state.gameplay.social.guild.votes = [ {
					id: 7,
					kind: 0,
					remainingMs: 60000,
					candidates: [ { id: 2, name: "Member" } ]
				} ];
			} ),
			beat( "vote", ctx => ctx.event( { kind: "activate", id: "npc-guild:vote" } ) )
		]
	},

	// --- the gesture tapes (stream: every beat must match) -------------------
	{
		name: "tape-inventory-drag",
		stream: true,
		beats: [
			beat( "open", ctx => {
				ctx.state.gameplay.inventory = [ POTION(), ELIXIR() ];
				ctx.event( { kind: "activate", id: "open-window:Inventory" } );
			} ),
			beat( "lift", ctx => ctx.event( { kind: "drag", id: "slot:13", dx: 36, dy: 0 } ) ),
			beat( "carry", ctx => ctx.event( { kind: "drag", id: "slot:13", dx: 36, dy: 38 } ) ),
			beat( "drop", ctx => ctx.event( { kind: "drag-end", id: "slot:14", x: 0, y: 0 } ) )
		]
	},
	{
		name: "tape-window-drag",
		stream: true,
		beats: [
			beat( "open", ctx => ctx.event( { kind: "activate", id: "open-window:Inventory" } ) ),
			beat( "drag-a", ctx => ctx.event( { kind: "drag", id: "window-drag:Inventory", dx: 120, dy: 60 } ) ),
			beat( "drag-b", ctx => ctx.event( { kind: "drag", id: "window-drag:Inventory", dx: -40, dy: 25 } ) ),
			beat( "drop", ctx => ctx.event( { kind: "drag-end", id: "window-drag:Inventory", x: 0, y: 0 } ) )
		]
	},
	{
		name: "tape-hotbar-drag",
		stream: true,
		beats: [
			beat( "settle", () => {} ),
			beat( "lift", ctx => ctx.event( { kind: "drag", id: "hotbar:1", dx: 4, dy: -20 } ) ),
			beat( "carry", ctx => ctx.event( { kind: "drag", id: "hotbar:1", dx: 40, dy: 0 } ) ),
			beat( "drop", ctx => ctx.event( { kind: "drag-end", id: "hotbar:1", x: 0, y: 0 } ) )
		]
	}
];
