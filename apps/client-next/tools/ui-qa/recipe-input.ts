/*
===========================================================================
recipe-input.ts - fixture inputs through observed production semantics

Recipes may seed snapshots but must not invent input targets or menu permissions.
===========================================================================
*/
import type { RecipeContext } from "./extended";
import { npcGid } from "../../src/engine/foundation/gameplay/npc-dialogue";

/*
================
press
================
*/
export async function press(
	context: RecipeContext,
	id: string,
	kind: "activate" | "double-activate" | "right-activate" = "activate"
) {
	await context.step();
	const control = context.controls().find( row => row.id === id );
	if ( !control || control.disabled ) throw Error( `Required visible enabled control unavailable: ${id}` );
	context.ui.event( { kind, id } );
	await context.step();
}

/*
================
openPanel

Explicit direct presentation mount, not proof of a preceding hotkey/menu path.
================
*/
export async function openPanel( context: RecipeContext, name: string ) {
	await context.step();
	context.ui.event( { kind: "activate", id: "open-window:" + name } );
	await context.step();
}

/*
================
npc
================
*/
export function npc( context: RecipeContext, capabilities: number, name = "Service official" ) {
	context.state.entities = context.state.entities.filter( ( row ) => row.gid !== 99 );
	const player = context.state.entities[0];
	if ( !player ) throw Error( "NPC recipe requires a player snapshot" );
	context.state.entities.push( { ...player, gid: 99, refObjId: 2011, kind: "npc", name } );
	Object.assign( context.state.gameplay, {
		target: 99,
		targetCapabilities: capabilities,
		npcConversation: { phase: "menu", gid: npcGid( 99 ), dialogueRevision: 1 }
	} );
}

/*
================
published
================
*/
export async function published( name: string ): Promise<unknown> {
	if ( !/^[a-zA-Z0-9-]+$/.test( name ) ) throw Error( "Invalid fixture data name" );
	const response = await fetch( `/assets/data/${name}.json` );
	if ( !response.ok ) throw Error( `Published fixture reference unavailable: ${name}` );
	return response.json();
}

/*
================
fixtureGuild
================
*/
export function fixtureGuild( context: RecipeContext ) {
	const guild = context.state.gameplay.social.guild;
	if ( !guild ) throw Error( "Guild recipe requires a guild snapshot" );
	return guild;
}

/*
================
guildMember
================
*/
export function guildMember( context: RecipeContext ) {
	const member = fixtureGuild( context ).members[0];
	if ( !member ) throw Error( "Guild recipe requires its local member" );
	return member;
}

export const herb = {
	slot: 14,
	refObjId: 3630,
	typeFlags: 0x8ec,
	quantity: 10,
	name: "HP recovery herb",
	icon: "item/etc/hp_potion_01.ddj",
	plus: 0,
	durability: 0,
	variance: "0",
	magic: []
};
