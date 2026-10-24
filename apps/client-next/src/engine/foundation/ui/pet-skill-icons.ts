/*
===========================================================================
pet-skill-icons.ts - port-only owned pet art, row help and server stack badges.

Each tier atlas has five skill families per pet. Simpler first/second books
lead into the unchanged third-book art, also used for the row information icon.
===========================================================================
*/
import type { PetTraining, PetTrainingSkill } from "@/engine/contracts/gameplay";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { PET_DESIGN } from "./pet-design-data";
import { petRowCooldown, type PetSkillDesign } from "./pet-training";

export const PET_SKILL_ATLAS = "/assets/images/Media_extracted/icon/port/pet-skills.png";

/*
================
petSkillIcon
================
*/
export function petSkillIcon( design: PetSkillDesign ): string {
	return design.option === 3 ? PET_SKILL_ATLAS : PET_SKILL_ATLAS.replace( ".png", `-${design.option}.png` );
}

/*
================
petSkillIconUV
================
*/
export function petSkillIconUV( design: PetSkillDesign ): UiRect {
	const row = [ "Wolf", "Bear", "Penguin", "Crow" ].indexOf( design.pet );
	return [ design.row / 5, row / 4, 1 / 5, 1 / 4 ];
}

/*
================
petSkillStack

Counters are observed server state, never inferred from animation or elapsed time.
================
*/
export function petSkillStack( state: PetTraining, design: PetSkillDesign, skill: PetTrainingSkill ): number | null {
	if ( !skill.live || skill.level === 0 || !skill.action && !skill.slotted ) return null;
	if ( design.family === "Stormbrood" ) return state.rows[design.row]?.left ?? 0;
	if ( design.family === "Tailwind" ) return state.tailwind?.stacks ?? 0;
	const c = state.counters;
	if ( !c ) return null;
	if ( design.family === "Unbroken Spirit" ) return c.resolve;
	if ( design.family === "Arcane Resonance" ) return c.penguinResonance ?? null;
	if ( design.family === "Frozen Hourglass" ) return c.hourglass ?? null;
	if ( design.family === "Bloodscent" ) return c.wolfBlood ?? null;
	if ( design.family === "Blood Hunt" ) return c.hunt ?? null;
	if ( design.family === "Execute" && design.option === 3 ) return c.wolfExecute ?? null;
	if ( design.family === "Crushing Paw" ) return c.bearHits;
	if ( design.family === "Guiding Feathers" ) return state.guidance?.stacks ?? 0;
	return null;
}

/*
================
petStackQuads

Same authored tab and numerals as skill levels, placed at the top right.
================
*/
export function petStackQuads( count: number | null, r: UiRect, clip: UiRect ): UiQuad[] {
	if ( count === null ) return [];
	const root = "/assets/images/Media_extracted/interface/skill/";
	const base = { uv: [ 0, 0, 1, 1 ] as UiRect, color: [ 1, 1, 1, 1 ] as const, clip };
	const scale = r[2] / 32;
	return [
		{
			...base,
			rect: [ r[0] + 18 * scale, r[1] - 2 * scale, 16 * scale, 16 * scale ],
			texture: root + "skl_level_tab.png"
		},
		...String( count ).split( "" ).map( ( digit, i ): UiQuad => ({
			...base,
			rect: [ r[0] + (count < 10 ? 26 : 23 + i * 5) * scale, r[1] + 2 * scale, 8 * scale, 8 * scale ],
			texture: root + "skl_lv_number_" + digit + ".png"
		}) )
	];
}

/*
================
petOwnerStacks

Readiness marks a full damage pool or the hit immediately before a release.
================
*/
export function petOwnerStacks( state: PetTraining | undefined ) {
	const counters = state?.counters;
	if ( !state || !counters ) return [];
	return state.skills.flatMap( skill => {
		const design = PET_DESIGN.treeSkills.find( d => d.id === skill.id );
		if ( !design || !skill.live || !skill.slotted || skill.level === 0 || design.option < 2 ) return [];
		let count: number | undefined, maximum: number, readyText: string;
		if ( design.family === "Arcane Resonance" ) {
			count = counters.ownerResonance;
			maximum = 5;
			readyText = "Maximum magical attack stacks";
		} else if ( design.family === "Frozen Hourglass" ) {
			count = counters.hourglass;
			maximum = 5;
			readyText = "Next penguin active recovers 2 seconds of cooldown";
		} else if ( design.family === "Bloodscent" ) {
			count = counters.ownerBlood;
			maximum = design.option === 3 ? 10 : 8;
			readyText = "Maximum damage stacks";
		} else if ( design.family === "Blood Hunt" ) {
			count = counters.hunt;
			maximum = 4;
			readyText = "Savage Rend on marked prey is ready";
		} else if ( design.family === "Execute" && design.option === 3 ) {
			count = counters.ownerExecute;
			maximum = 7;
			readyText = "Next damaging attack triggers the execution sweep";
		} else if ( design.family === "Crushing Paw" ) {
			count = counters.ownerHits;
			maximum = 10;
			readyText = "Next qualifying hit releases the stun";
		} else return [];
		if ( count === undefined ) return [];
		const ready = count >=
			maximum -
				([ "Bloodscent", "Blood Hunt", "Arcane Resonance", "Frozen Hourglass" ].includes( design.family ) ?
					0 :
					1);
		const status = ready ? readyText : "Building stacks";
		return [ {
			design,
			count,
			maximum,
			ready,
			label: `Owner ${design.name}: ${count}/${maximum} â€” ${status}`,
			helpText: `Owner Â· ${design.name}\n${count} / ${maximum}\n${status}\n` +
				(design.family === "Blood Hunt" ?
					"Shared prey mark. Rend the marked enemy; expires after 8 s. Switching prey resets charges." :
					design.family === "Bloodscent" ?
					"Attacks refresh stacks for 6 s." :
					"Triggers automatically; no button press required.")
		} ];
	} );
}

/*
================
petRowHelp
================
*/
export function petRowHelp( state: PetTraining, row: number, now: number ): string {
	const skills = state.skills.filter( s => PET_DESIGN.treeSkills.find( d => d.id === s.id )?.row === row );
	const chosen = skills.find( s => s.slotted || s.queued );
	const d = PET_DESIGN.treeSkills.find( entry => entry.id === (chosen ?? skills[0])?.id );
	const lines = [
		d?.family ?? "Pet skills",
		...(row < 2 ?
			[
				"Click to cast.",
				"Drag to numbered quickslots.",
				"Row books share recharge.",
				"Recharge: " + (petRowCooldown( state, row, now ) || "Ready")
			] :
			[
				"Highest learned book is always active.",
				"Earlier books are replaced automatically.",
				chosen ? "Active: " + d?.name : "No passive active"
			]),
		"Unlocks at pet 10 / 40 / 90.",
		"Master each book to advance."
	];
	if ( d?.family === "Crushing Paw" ) {
		lines.push( `Bear hits: ${state.counters?.bearHits ?? 0} / 10` );
		if ( d.option > 1 ) lines.push( `Owner hits: ${state.counters?.ownerHits ?? 0} / 10` );
		lines.push(
			"Top right: bear hits.",
			"Physical/spell hits; shields count.",
			"Blocks and DoTs do not count.",
			"Unique monsters resist stun."
		);
	}
	if ( d?.family === "Unbroken Spirit" ) {
		lines.push(
			"Top right: Resolve stacks.",
			"Landed monster hits grant stacks.",
			"Blocks and DoTs do not count.",
			"Expiry/cast removes bonus HP."
		);
	}
	if ( d?.family === "Bloodscent" ) {
		lines.push(
			`Wolf stacks: ${state.counters?.wolfBlood ?? 0}`,
			`Owner stacks: ${state.counters?.ownerBlood ?? 0}`,
			"Top right: wolf stacks.",
			"One stack per damaging action.",
			"Blocks, DoTs and procs add none.",
			"Both pools expire after 6 s."
		);
	}
	if ( d?.family === "Hunting Howl" ) {
		lines.push( "Manual buff; no enemy required.", "Strongest howl wins; no stacking." );
	}
	if ( d?.family === "Blood Hunt" ) {
		lines.push(
			`Prey mark: ${state.counters?.hunt ?? 0} / 4`,
			"Top right: shared prey charges.",
			"Switching prey resets charges.",
			"Rend consumes a ready mark.",
			"Mark expires after 8 s.",
			"Blocks, DoTs and procs add none."
		);
	}
	if ( d?.family === "Execute" ) {
		lines.push( "Unique monsters cannot be executed." );
		if ( d.option > 1 ) lines.push( "Owner requires a living wolf within 20 m.", "Ordinary-world PvE only." );
		if ( d.option === 3 ) {
			lines.push(
				`Wolf attacks: ${state.counters?.wolfExecute ?? 0} / 7`,
				`Owner attacks: ${state.counters?.ownerExecute ?? 0} / 7`,
				"Top right: wolf attack count.",
				"Each damaging action counts once.",
				"Sweeps/DoTs/procs do not count."
			);
		}
	}
	if ( d?.family === "Guiding Feathers" ) {
		lines.push( "Top right: Precision stacks.", "Five stacks: next damaging skill empowered." );
	}
	if ( d?.pet === "Bear" ) lines.push( "PvE only; unavailable in town.", "No jobs, PvP or instances." );
	if ( d?.family === "Challenging Roar" ) {
		lines.push( "No target required; no damage.", "Unique monsters resist taunt." );
	}
	if ( d?.family === "Guardian's Bond" ) {
		lines.push(
			"Revival recharge is shared.",
			"Persists through resummon/relog.",
			"Damage transfer follows shields."
		);
	}
	return lines.join( "\n" );
}
