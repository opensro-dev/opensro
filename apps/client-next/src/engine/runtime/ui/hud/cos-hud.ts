/*
===========================================================================

cos-hud.ts - the COS HUD's state: selected companion, bar and Clean prompt

CIFCOSManager shares one status icon across guild soldiers; other companions
have individual status icons. The worker publishes their bindings and selected
GID. The HUD owns one command panel, shown for the selected companion (CIFCOSManager_SelectCompanion
6F21F0). This module mirrors selection and owns the panel's open/close toggle
(CIFCOSCommand_OnToggleOpen 6A3750), the transport Clean confirmation
the executor raises (6A2350 case 5), and the item-target cursor A6 that
Grass of Life, the clock and the armour gender tool arm on right-button use.
The UI draws from it every frame.

Port-only, not native: the weapon switch scroll (weapon-switch.ts) arms
the same cursor; its clicked weapon opens a box instead of the Yes/No
confirmation that asks the race, then the kind of that race, and the
picked kind completes the use.

===========================================================================
*/
import type { GameplayCommand, InventoryItem, CosRecord } from "@/engine/contracts/gameplay";
import { cosClass } from "@/engine/foundation/ui/cos-command";
import { companionItemTargetCommand } from "@/engine/foundation/gameplay/cos-item-use";
import {
	isWeaponSwitchScroll,
	weaponKind,
	weaponSwitchKinds,
	WEAPON_RACES
} from "@/engine/foundation/gameplay/weapon-switch";
import { raceSwitchAccepts, raceSwitchKindOf } from "@/engine/foundation/gameplay/race-switch";

const ITEM_TARGET_CURSOR = 0xa6;

/*
================
createCosHud
================
*/
export function createCosHud() {
	let selected = 0, open = true, cleanConfirm: number | null = null;
	let source: InventoryItem | null = null, targetUse: GameplayCommand | null = null;
	// The weapon switch box (port-only): the chosen weapon's type word and
	// the race picked in its first step, null until then.
	let switchType = 0, switchRace: number | null = null;
	/*
	================
	switchKinds

	The kinds the box's second step offers, empty before a race is picked.
	================
	*/
	function switchKinds(): readonly number[] {
		if ( switchType === 0 || !targetUse || switchRace === null ) return [];
		return weaponSwitchKinds( switchType, switchRace );
	}
	/*
	================
	resetSwitch
	================
	*/
	function resetSwitch() {
		switchType = 0;
		switchRace = null;
	}
	return {
		/*
		================
		armItemTarget

		561D50 stores the selected inventory slot (596B50), then sets cursor A6
		at 5621D4, shared by Grass of Life, the clock and the armour gender tool
		(armsItemTargetCursor). Arming never sends item use or chooses a target.
		================
		*/
		armItemTarget( item: InventoryItem ) {
			source = item;
			targetUse = null;
			resetSwitch();
		},
		/*
		================
		itemTargetCursor
		================
		*/
		itemTargetCursor(): 0xa6 | null {
			return source && !targetUse ? ITEM_TARGET_CURSOR : null;
		},
		/*
		================
		itemTargetSource

		The item the armed target cursor will act for, or null while it
		is not armed. An avatar-window click cannot travel the bag-target
		path (chooseItemTarget), so the caller reads the source's type
		word itself to answer one: the extension gear aimed at the worn
		Spirit (spiritExtensionWornNotice).
		================
		*/
		itemTargetSource(): InventoryItem | null {
			return source && !targetUse ? source : null;
		},
		/*
		================
		chooseItemTarget

		567372 checks cursor A6: any occupied inventory or equipment slot opens
		the kind-0x1F confirmation (pushed at 5673A9) and clears the cursor.
		Kind 0x1F asks UIIT_MSG_QUESTION_SILKMALL_ITEM_USE (53252D), the same
		use question as the mall gate. Its Yes runs
		CIFInventory_ExecuteItemAction(source, target, slot) (697B79), as a
		drop on that slot does; the worker applies 6968BA's target checks.
		================
		*/
		chooseItemTarget( item: InventoryItem | undefined ) {
			if ( !source || !item || targetUse ) return false;
			if ( isWeaponSwitchScroll( source.codename ) ) {
				// Port-only: a bag weapon opens the race step; anything else
				// leaves the cursor armed for another click.
				if ( item.slot < 13 || weaponKind( item.typeFlags ) === 0 ) return false;
				switchType = item.typeFlags;
				switchRace = null;
				targetUse = { kind: "item-use", slot: source.slot, targetSlot: item.slot };
				return true;
			}
			const raceKind = raceSwitchKindOf( source.codename );
			if ( raceKind ) {
				// Port-only: a bag item the race switch scroll takes opens
				// the Yes/No confirmation; anything else leaves the cursor
				// armed for another click.
				if ( item.slot < 13 || !raceSwitchAccepts( raceKind, item.typeFlags ) ) return false;
				targetUse = { kind: "item-use", slot: source.slot, targetSlot: item.slot };
				return true;
			}
			targetUse = companionItemTargetCommand( source, item );
			return targetUse !== null;
		},
		/*
		================
		weaponSwitchRaces

		The races the weapon switch box's first step offers, empty when the
		box is not up or a race is already picked.
		================
		*/
		weaponSwitchRaces(): readonly number[] {
			return switchType !== 0 && targetUse && switchRace === null ? WEAPON_RACES : [];
		},
		/*
		================
		chooseWeaponRace

		The first step's pick; false for a race the box does not offer.
		================
		*/
		chooseWeaponRace( race: number ): boolean {
			if ( switchType === 0 || !targetUse || switchRace !== null || !WEAPON_RACES.includes( race ) ) return false;
			switchRace = race;
			return true;
		},
		/*
		================
		backWeaponRace

		The second step's Back: the race step again, the weapon kept.
		================
		*/
		backWeaponRace(): boolean {
			if ( switchType === 0 || !targetUse || switchRace === null ) return false;
			switchRace = null;
			return true;
		},
		/*
		================
		weaponSwitchKinds
		================
		*/
		weaponSwitchKinds(): readonly number[] {
			return switchKinds();
		},
		/*
		================
		chooseWeaponKind

		The second step's pick: the use with its kind, or null for a kind the
		box does not offer.
		================
		*/
		chooseWeaponKind( kind: number ): GameplayCommand | null {
			if ( !targetUse || targetUse.kind !== "item-use" || !switchKinds().includes( kind ) ) return null;
			const command: GameplayCommand = { ...targetUse, weaponKind: kind };
			source = null;
			targetUse = null;
			resetSwitch();
			return command;
		},
		/*
		================
		targetUse
		================
		*/
		targetUse() {
			return targetUse;
		},
		/*
		================
		targetUseBox

		Each targeted item's confirmation owns its control ids, so no two
		boxes ever publish the same control.
		================
		*/
		targetUseBox(): "cos-revive" | "cos-renew" | "gender-change" | "weapon-switch" | "race-switch" | null {
			if ( !targetUse || targetUse.kind !== "item-use" ) return null;
			if ( source && isWeaponSwitchScroll( source.codename ) ) return "weapon-switch";
			if ( source && raceSwitchKindOf( source.codename ) ) return "race-switch";
			if ( targetUse.targetSlot !== undefined ) return "gender-change";
			return targetUse.revivalSlot !== undefined ? "cos-revive" : "cos-renew";
		},
		/*
		================
		takeTargetUse
		================
		*/
		takeTargetUse() {
			const command = targetUse;
			source = null;
			targetUse = null;
			resetSwitch();
			return command;
		},
		/*
		================
		reconcileItemTarget

		The native cursor is global: hotbar use can arm it with inventory closed.
		A changed source slot or departure from the world cancels the selection.
		================
		*/
		reconcileItemTarget( items: readonly InventoryItem[], visible: boolean ) {
			const armed = source;
			if (
				!armed ||
				visible && items.some( item =>
						item.slot === armed.slot && item.refObjId === armed.refObjId &&
						item.typeFlags === armed.typeFlags && item.quantity > 0
					)
			) return false;
			source = null;
			targetUse = null;
			return true;
		},

		/*
		================
		reconcile

		The manager selects a newly added companion (CIFCOSManager_AddCompanion
		6F2710 calls SelectCompanion) and falls back to the first remaining one
		when the selected companion is removed (6F2900). Returns whether the
		state changed.
		================
		*/
		reconcile( records: readonly CosRecord[], selectedGid?: number ) {
			const shown = records.filter( r => cosClass( r.band ) !== null );
			let changed = selectedGid !== undefined && selected !== selectedGid;
			if ( selectedGid !== undefined ) selected = selectedGid;
			if ( selectedGid === undefined && !shown.some( r => r.gid === selected ) ) {
				const next = shown.at( -1 )?.gid ?? 0;
				changed = next !== selected;
				selected = next;
			}
			if ( cleanConfirm !== null && !shown.some( r => r.gid === cleanConfirm ) ) {
				cleanConfirm = null;
				changed = true;
			}
			return changed;
		},
		/*
================
reset
================
		*/
		reset() {
			selected = 0;
			open = true;
			cleanConfirm = null;
			source = null;
			targetUse = null;
		},
		/*
================
select
================
		*/
		select( gid: number ) {
			selected = gid;
		},
		/*
================
selected
================
		*/
		selected() {
			return selected;
		},
		/*
================
toggle
================
		*/
		toggle() {
			open = !open;
		},
		/*
================
open
================
		*/
		open() {
			return open;
		},
		/*
================
askClean
================
		*/
		askClean( gid: number ) {
			cleanConfirm = gid;
		},
		/*
================
cleanConfirm
================
		*/
		cleanConfirm() {
			return cleanConfirm;
		},
		/*
================
takeClean
================
		*/
		takeClean() {
			const gid = cleanConfirm;
			cleanConfirm = null;
			return gid;
		}
	};
}
