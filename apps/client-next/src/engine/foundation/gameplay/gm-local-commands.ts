/*
===========================================================================

gm-local-commands.ts - the GM commands the client executes itself

ChatInput_HandleGmCommand (SRO_Client 509CF0) mixes two families: the
0x75B6 server commands (gm-command.ts) and commands the client runs
locally with no server frame at all. This module owns the local family,
each arm traced in 509CF0:

- /TEXTCHAT <key> resolves a textdata key (CTextStringManager_
  GetTextByWString 796330) and sends the text as an ordinary chat line
  through NetClient_Send7367 (694490) with chat type 1 (All). The server
  already forces the GM broadcast type for a privileged speaker, so no
  server half exists. The console resolves the key (hudCopy) and sends
  through the ordinary chat command; this module only classifies.
- /SMSG is matched and swallowed by 509CF0 (@0x50C127 destroys the
  stream buffer and returns handled): the chat composer sends nothing
  for it. The GM console is the port's GM text surface, so the console
  sends the literal text as a chat line, exactly like /TEXTCHAT.
- INPUT_CHAT <arg> opens "<client data path>ChatString.txt"
  (@0x50BFC2 reads [0xF0C8E8]+0x134, @0x50BFE3 appends the literal at
  0xBCC7A0) and broadcasts its lines. The port ships no ChatString.txt
  asset, so the console answers with the native missing-file line
  ("ChatString Error. -> %s") and sends nothing.
- /GMSKILL casts SKILL_OP_HARMONY_A_01: the argument is read with
  CRT_swscanf "%d" (@0x509DF1); a zero or unparseable argument resolves
  the codename (GlobalDataManager_FindSkillIdByName 916830) and casts
  through CGInterface_ExecuteSelectedActionAtTarget (6FCD50 @0x509E1D).
  A nonzero argument jumps past the cast (@0x509DFF): handled, no-op.
- /useskill <id> casts the parsed id through the same 6FCD50 door
  (@0x50CC08); a zero parse is handled with no cast.
- /GACHASTART walks the open gacha control's registered cards and sends
  one 0x7053 byte per card: CIFControl_GetSlotIndex378 (54FC30) + 0x0D
  (@0x50C6D7; 0x0D is the bag-slot base, the same +0xD the window adds
  to a card index). The port's roll authority is the nine-byte 0x7053
  the window itself sends (item/gacha wire OpRoll), so the console runs
  the window's own play path instead of composing the one-byte form;
  the server executes the same ticket roll either way.
- IMPLE_COMMAND_ADD_WP / DEL_WP / SHOW_WP keep named waypoints in a
  client-side list (@0x50AC2A: the vector at +0x480 of the interface
  object). A waypoint is the /warp record: region, X, Y, Z and heading
  (@0x50AC73 reads them from the live player object). ADD prints
  "New WP : %s" and "Save WP Count : %d"; /warp <name> composes the
  0x75B6 coordinate warp (subcmd 0x10) from the stored record and a bad
  name prints "Illigal WP Name" [sic].
- /window <n> writes the registry value "window" under
  Software\Joymax\Silkroad (@0x50BA43) and prints "Window Mode(Please
  Game Restart)" for nonzero, "FullScreen Mode(Please Game Restart)"
  for zero. The port maps the write onto its persisted windowMode game
  option; the console prints the same two lines.

Every arm here runs only inside the GM console, which both the UI and
the worker gate on the session's GM privilege (eligibility.gm). A non-GM
session never reaches this module: that is the native refusal surface
for client-local commands (the server refuses the 0x75B6 family itself).

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

const OP_GM_COMMAND = 0x75b6;
const GM_WARP_SUBCOMMAND = 0x10;
const MAX_WAYPOINT_NAME = 64;

/*
================
GmWaypoint

One saved waypoint: the /warp record (region, position, heading) under
the name the GM typed. The list is client-local state; the port keeps it
for the session in the worker.
================
*/
export interface GmWaypoint {
	readonly name: string;
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
	readonly heading: number;
}

/*
================
GmLocalCommand

One classified console line. The executors live with the state each arm
touches: chat text and the window option in the UI (ui.ts), casts,
gacha and waypoints in the worker (gameplay.ts).
================
*/
export type GmLocalCommand =
	| { readonly type: "textchat"; readonly key: string; }
	| { readonly type: "smsg"; readonly text: string; }
	| { readonly type: "inputchat"; }
	| { readonly type: "useskill"; readonly skillId: number; }
	| { readonly type: "gmskill"; }
	| { readonly type: "consumed"; }
	| { readonly type: "gachastart"; }
	| { readonly type: "addwp"; readonly name: string; }
	| { readonly type: "delwp"; readonly name: string; }
	| { readonly type: "showwp"; }
	| { readonly type: "warpwp"; readonly name: string; }
	| { readonly type: "window"; readonly windowed: boolean; };

/*
================
gmLocalCommand

Classify one GM console line into the local family. Matching folds case
the way CRT_wcsicmp does in 509CF0. A line outside the family (every
0x75B6 command, every non-command) returns null; a line the native
composer matches but leaves unexecuted returns "consumed".
================
*/
export function gmLocalCommand( line: string ): GmLocalCommand | null {
	const tokens = line.trim().split( /\s+/ );
	const head = (tokens[0] ?? "").toUpperCase();
	const bare = head.startsWith( "/" ) ? head.slice( 1 ) : head;
	if ( head === "/TEXTCHAT" ) {
		return tokens.length === 2 ? { type: "textchat", key: tokens[1]! } : null;
	}
	if ( head === "/SMSG" ) {
		if ( tokens.length < 2 ) return { type: "consumed" };
		return { type: "smsg", text: line.trim().slice( tokens[0]!.length ).trim() };
	}
	if ( bare === "INPUT_CHAT" ) {
		return tokens.length <= 2 ? { type: "inputchat" } : null;
	}
	if ( head === "/USESKILL" ) {
		if ( tokens.length !== 2 ) return null;
		const skillId = Number.parseInt( tokens[1]!, 10 );
		return Number.isInteger( skillId ) && skillId > 0 ? { type: "useskill", skillId } : { type: "consumed" };
	}
	if ( head === "/GMSKILL" ) {
		if ( tokens.length > 2 ) return null;
		const parsed = tokens.length === 2 ? Number.parseInt( tokens[1]!, 10 ) : 0;
		return parsed === 0 || !Number.isInteger( parsed ) ? { type: "gmskill" } : { type: "consumed" };
	}
	if ( head === "/GACHASTART" ) {
		return tokens.length === 1 ? { type: "gachastart" } : null;
	}
	if ( bare === "IMPLE_COMMAND_ADD_WP" || bare === "IMPLE_COMMAND_DEL_WP" ) {
		if ( tokens.length !== 2 || tokens[1]!.length > MAX_WAYPOINT_NAME ) return null;
		return bare === "IMPLE_COMMAND_ADD_WP" ?
			{ type: "addwp", name: tokens[1]! } :
			{ type: "delwp", name: tokens[1]! };
	}
	if ( bare === "IMPLE_COMMAND_SHOW_WP" ) {
		return tokens.length === 1 ? { type: "showwp" } : null;
	}
	if ( head === "/WARP" ) {
		// The coordinate form (five tokens) belongs to gmRequest; the
		// two-token form warps to a saved waypoint.
		return tokens.length === 2 ? { type: "warpwp", name: tokens[1]! } : null;
	}
	if ( head === "/WINDOW" ) {
		if ( tokens.length !== 2 ) return null;
		const value = Number.parseInt( tokens[1]!, 10 );
		return { type: "window", windowed: (Number.isInteger( value ) ? value : 0) !== 0 };
	}
	return null;
}

/*
================
waypointIndex

The named waypoint's slot, folding case like every other name path in
the client. -1 when the name was never saved.
================
*/
function waypointIndex( waypoints: readonly GmWaypoint[], name: string ): number {
	const folded = name.toUpperCase();
	return waypoints.findIndex( waypoint => waypoint.name.toUpperCase() === folded );
}

/*
================
gmWaypointAdd

Save the current spot under a name (an existing name is re-saved in
place). The prints are the native console lines: "New WP : %s" for a
new name and "Save WP Count : %d" after every save.
================
*/
export function gmWaypointAdd(
	waypoints: readonly GmWaypoint[],
	spot: GmWaypoint
): { readonly waypoints: readonly GmWaypoint[]; readonly prints: readonly string[]; } {
	const at = waypointIndex( waypoints, spot.name );
	if ( at >= 0 ) {
		const next = [ ...waypoints ];
		next[at] = spot;
		return { waypoints: next, prints: [ `Save WP Count : ${next.length}` ] };
	}
	const next = [ ...waypoints, spot ];
	return { waypoints: next, prints: [ `New WP : ${spot.name}`, `Save WP Count : ${next.length}` ] };
}

/*
================
gmWaypointDelete

Drop a saved waypoint. An unknown name answers with the same line a bad
/warp name gets ("Illigal WP Name", the native spelling).
================
*/
export function gmWaypointDelete(
	waypoints: readonly GmWaypoint[],
	name: string
): { readonly waypoints: readonly GmWaypoint[]; readonly prints: readonly string[]; } {
	const at = waypointIndex( waypoints, name );
	if ( at < 0 ) return { waypoints, prints: [ "Illigal WP Name" ] };
	const next = waypoints.filter( ( _, index ) => index !== at );
	return { waypoints: next, prints: [ `Save WP Count : ${next.length}` ] };
}

/*
================
gmWaypointShow

The saved list as console lines: the count, then one name per waypoint
in save order.
================
*/
export function gmWaypointShow( waypoints: readonly GmWaypoint[] ): readonly string[] {
	return [ `Save WP Count : ${waypoints.length}`, ...waypoints.map( waypoint => waypoint.name ) ];
}

/*
================
gmWaypointWarpFrame

The 0x75B6 coordinate-warp frame (subcmd 0x10, the 50B303 layout) for a
saved waypoint, or the "Illigal WP Name" print when the name is unknown.
================
*/
export function gmWaypointWarpFrame(
	waypoints: readonly GmWaypoint[],
	name: string
): { readonly frame?: WireFrame; readonly prints: readonly string[]; } {
	const at = waypointIndex( waypoints, name );
	if ( at < 0 ) return { prints: [ "Illigal WP Name" ] };
	const spot = waypoints[at]!;
	const payload = new Uint8Array( 17 ), view = new DataView( payload.buffer );
	payload[0] = GM_WARP_SUBCOMMAND;
	view.setUint16( 1, spot.regionId, true );
	view.setFloat32( 3, spot.x, true );
	view.setFloat32( 7, spot.y, true );
	view.setFloat32( 11, spot.z, true );
	view.setUint16( 15, spot.heading, true );
	return { frame: { opcode: OP_GM_COMMAND, payload }, prints: [] };
}
