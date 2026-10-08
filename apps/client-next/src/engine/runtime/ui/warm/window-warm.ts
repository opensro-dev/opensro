/*
===========================================================================

window-warm.ts - build every window once, unseen, so its first open is instant

The retail client loads interface art synchronously from local PK2, so any
window opens on the frame its key is pressed. This port fetches and decodes
art on demand, and a window is admitted only once every image it draws has
decoded; the first press of I, S, C, M and the rest therefore waited on a
burst of fetch and decode work.

After world entry, while no window is open, the UI builds one window per
frame through its own draw code and keeps only the image demand that build
produced. That demand stays wanted for the session, so the images remain
decoded and every later open is admitted on its first frame. The frame
that builds a window publishes nothing; the previous product stays on
screen for that one frame.

===========================================================================
*/
import type { UiPanel } from "@/engine/foundation/ui/panels";
import type { SkillUi } from "@/engine/foundation/ui/skill-layout";

/*
================
WARM_WINDOWS

Windows whose opening is a pure view. Shop, COS inventory, Alchemy, Magic
Pop and the two matching boards open server workflows (or need a live
counterpart); Academy and Game Guide raise guide events while drawn. They
are left out: their authored art is in the HUD and guide warm sets.
================
*/
const WARM_WINDOWS = [
	"Inventory",
	"Character",
	"Skills",
	"Actions",
	"Quests",
	"Map",
	"Party",
	"Guild",
	"Guild tools",
	"Chat",
	"Blocking",
	"Option",
	"Auto Potion",
	"System"
] as const;

/*
================
createWindowWarm
================
*/
export function createWindowWarm( windows: readonly UiPanel[] = WARM_WINDOWS ) {
	let next = 0, current: UiPanel | null = null;
	const demand = new Set<string>();
	return {
		/*
================
begin

The window to build unseen this frame, or null when every window is warm
or the caller is not idle.
================
		*/
		begin( idle: boolean ): UiPanel | null {
			if ( current !== null ) throw new Error( "Window warm frame already open" );
			if ( !idle || next >= windows.length ) return null;
			current = windows[next++]!;
			return current;
		},
		/*
================
end

Keeps the image demand of the frame that built the window.
================
		*/
		end( paths: readonly string[] ) {
			if ( current === null ) throw new Error( "No window warm frame open" );
			for ( const path of paths ) demand.add( path );
			current = null;
		},
		/*
================
add

Extra demand a window reaches only through interaction (another mastery
tab, for example).
================
		*/
		add( paths: Iterable<string> ) {
			for ( const path of paths ) demand.add( path );
		},
		active: () => current !== null,
		paths: () => demand,
		/*
================
reset

A new world session warms again (race, masteries and items may differ).
================
		*/
		reset() {
			next = 0;
			current = null;
			demand.clear();
		}
	};
}

/*
================
skillWindowIcons

The raw icon names the skill window can draw for the given masteries: each
mastery's own icon, its groups' icons and every skill slot. The window
draws one mastery tab at a time, so a warm build of it reaches only the
selected mastery; this covers the other tabs.
================
*/
export function skillWindowIcons(
	catalog: SkillUi,
	masteries: readonly number[]
): { icons: string[]; groups: string[]; } {
	const owned = new Set( masteries );
	const icons = new Set<string>(), groups = new Set<string>();
	for ( const mastery of catalog.masteries ) if ( owned.has( mastery.id ) ) icons.add( mastery.icon );
	for ( const group of catalog.groups ) if ( owned.has( group.mastery ) ) groups.add( group.icon );
	for ( const skill of catalog.skills ) if ( owned.has( skill.mastery ) ) icons.add( skill.icon );
	return { icons: [ ...icons ], groups: [ ...groups ] };
}
