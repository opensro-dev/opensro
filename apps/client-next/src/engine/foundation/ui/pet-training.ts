/*
===========================================================================

pet-training.ts - how the pet window presents the attack-pet tree and gear

Port-only, not native (the server's SRO_ATTACK_PET_EXPANSION and the
Experimental window's attack-pet training row). The server decides every
rule and sends each skill's level, requirement and effect values (0x3FB1);
names, effect text, ranks, icons and affix text come from the design
workbook's export (pet-design-data.ts). Pure.

===========================================================================
*/
import type { PetTraining, PetTrainingSkill } from "@/engine/contracts/gameplay";
import { PET_DESIGN } from "./pet-design-data";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { quickslotCooldownQuads } from "./quickslot-cooldown";
import type { TooltipRow } from "./tooltip-rows";
import type { CosReference, cosAbilities } from "./cos-command";

/*
================
petReference

Port-only stats use the server's catalog and worn/passive modifiers.
================
*/
export function petReference(
	reference: CosReference | undefined,
	training: PetTraining | undefined
): CosReference | undefined {
	if ( !reference || !training?.stats ) return reference;
	return {
		...reference,
		maxHp: training.stats.maxHp,
		physicalDefence: Math.trunc(
			(reference.physicalDefence + (training.stats.gearStats?.[2] ?? 0)) * training.defensePercent / 100
		),
		magicalDefence: Math.trunc(
			(reference.magicalDefence + (training.stats.gearStats?.[3] ?? 0)) * training.stats.magicDefensePercent / 100
		),
		hit: reference.hit + (training.stats.gearStats?.[4] ?? 0),
		parry: reference.parry + (training.stats.gearStats?.[5] ?? 0)
	};
}

/*
================
petAbilities
================
*/
export function petAbilities(
	ability: ReturnType<typeof cosAbilities>,
	training: PetTraining | undefined
): ReturnType<typeof cosAbilities> {
	if ( !training?.stats ) return ability;
	const scale = (
		range: readonly [number, number],
		pct: number
	): readonly [number, number] => [ Math.trunc( range[0] * pct / 100 ), Math.trunc( range[1] * pct / 100 ) ];
	const magic = ability.magical[1] === 0 && training.stats.magicPrimary ? ability.physical : ability.magical;
	return {
		...ability,
		physical: training.stats.magicPrimary ?
			[ 0, 0 ] :
			scale( [
				ability.physical[0] + (training.stats.gearStats?.[0] ?? 0),
				ability.physical[1] + (training.stats.gearStats?.[0] ?? 0)
			], training.attackPercent ),
		magical: scale( [
			magic[0] + (training.stats.gearStats?.[1] ?? 0),
			magic[1] + (training.stats.gearStats?.[1] ?? 0)
		], training.stats.magicPercent )
	};
}

// The server's training operations (action/pettraining.go).
export const PET_TRAINING_OP = { state: 0, learn: 1, use: 2, equip: 3, unequip: 4, refund: 5, slot: 6 } as const;

// Blue options of pet gear: param id = base + affix code (companion/gear.go).
export const PET_AFFIX_PARAM_BASE = 0x8000;

export type PetSkillDesign = (typeof PET_DESIGN.treeSkills)[number];

// The tree's rows and the gear slots, with the character equipment socket
// art each slot borrows (necklace, chest, ring).
export const PET_ROW_NAMES = [ "Active I", "Active II", "Passive I", "Passive II", "Passive III" ] as const;
export const PET_GEAR_SLOTS: readonly { readonly name: string; readonly socket: number; }[] = [
	{ name: "Collar", socket: 10 },
	{ name: "Harness", socket: 1 },
	{ name: "Charm", socket: 11 }
];

/*
================
petSkillDesign
================
*/
export function petSkillDesign( id: number ): PetSkillDesign | undefined {
	return PET_DESIGN.treeSkills.find( s => s.id === id );
}

/*
================
petRows

The pet's skills laid out by row (0-1 actions, 2-4 passives), each row in
option order, from the state's skill list.
================
*/
export function petRows( state: PetTraining ): { design: PetSkillDesign; skill: PetTrainingSkill; }[][] {
	const rows: { design: PetSkillDesign; skill: PetTrainingSkill; }[][] = [ [], [], [], [], [] ];
	for ( const skill of state.skills ) {
		const design = petSkillDesign( skill.id );
		if ( design ) rows[design.row]!.push( { design, skill } );
	}
	for ( const row of rows ) row.sort( ( a, b ) => a.design.option - b.design.option );
	return rows;
}

/*
================
petPetName

The pet kind of a state ("Wolf", ...), from its first skill.
================
*/
export function petPetName( state: PetTraining ): string {
	return petSkillDesign( state.skills[0]?.id ?? 0 )?.pet ?? "";
}

/*
================
formatValue
================
*/
function formatValue( value: number ): string {
	return Number.isInteger( value ) ? String( value ) : value.toFixed( 1 ).replace( /\.0$/, "" );
}

/*
================
petEffect

A skill's effect text at a value ({v} substituted).
================
*/
export function petEffect( design: PetSkillDesign, value: number, effectiveLevel = 1 ): string {
	const rank2 = effectiveLevel >= PET_DESIGN.rules.rank2Level || design.option > 1;
	const rank3 = effectiveLevel >= PET_DESIGN.rules.rank3Level || design.option > 1;
	if ( design.family === "Mend" ) {
		return `On command, heals the owner for ${
			formatValue( value * (rank2 ? 1.5 : 1) )
		}% of maximum HP immediately.`;
	}
	if ( design.family === "Mark Prey" ) {
		return `Commanded strike attempts to reduce physical and magical defense by ${
			formatValue( value + (rank2 ? 20 : 0) )
		}% for 8 s.`;
	}
	if ( design.family === "Terrifying Howl" ) {
		return `Commanded strike terrifies up to ${Math.max( 2, Math.floor( value ) )} monsters within ${
			rank2 ? 9 : 6
		} m for ${rank3 ? 4 : 3} s; affected monsters cannot attack the wolf.`;
	}
	if ( design.family === "Frost Nova" ) {
		return `Commanded strike deals ${formatValue( value )}% magical damage to up to 8 targets within ${
			rank2 ? 7 : 5
		} m and attempts frostbite. Status duration follows native resistance rules.`;
	}
	if ( design.family === "Rending Bite" ) {
		return `Commanded strike deals ${formatValue( value )}% damage and applies bleeding: ${
			rank2 ? 4 : 2
		} x pet level every 2 s for 6 s.` +
			(rank3 ? " Bleeding non-unique targets below 30% HP are executed on a damaging hit." : "");
	}
	if ( design.family === "Crushing Paw" ) {
		return `Every ${rank2 ? 5 : 6}th damaging attack attempts a ${formatValue( value )} s stun` +
			(rank3 ? " on up to 5 targets within 3 m." : " on the target.");
	}
	if ( design.family === "Static Feathers" ) {
		return `${formatValue( value )}% chance per attack to spread that strike to up to ${
			rank2 ? 5 : 3
		} targets within 8 m, with 20% damage reduction and an electric-shock attempt. No separate lightning spell is cast.`;
	}
	if ( design.family === "Murder of Crows" ) {
		return `Every ${rank2 ? 8 : 10}th damaging attack deals ${
			formatValue( value )
		}% damage to up to 8 targets within 4 m.` +
			(rank3 ? " Also attempts 2 s Myopia (50% attack-range reduction)." : "");
	}
	if ( design.family === "Challenging Roar" ) {
		const radius = 8 + (design.option - 1) * 4 + (Math.max( 1, effectiveLevel ) - 1) * 0.4;
		return `Taunts up to ${design.option === 3 ? 6 : 3} monsters within ${formatValue( radius )} m. ` +
			(design.option >= 2 ? "Party monsters allowed. " : "Party monsters require Tier II. ") +
			"Unique monsters are immune. No damage.";
	}
	const shown = value;
	const effect = design.family === "Last Stand" && rank2 ?
		design.effect.replace( "30% HP", "40% HP" ) :
		design.effect;
	return effect.replaceAll( "{v}", formatValue( shown ) );
}

/*
================
petCooldownQuads

Reuse character skill sweep and digits for the server's shared row timer.
Counter recharge stays numeric in the row label; it is not elapsed time.
================
*/
export function petCooldownQuads(
	state: PetTraining,
	row: number,
	nowMs: number,
	rect: UiRect,
	clip: UiRect
): UiQuad[] {
	const cooldown = state.rows[row];
	if ( !cooldown || cooldown.timerMs <= 0 ) return [];
	return quickslotCooldownQuads(
		[ {
			skill: row + 1,
			group: 0,
			startedAtMs: state.receivedAtMs,
			durationMs: cooldown.timerMs
		} ],
		row + 1,
		0,
		nowMs,
		rect,
		clip
	);
}

/*
================
petRowCooldown

An active row's cooldown as text: "" when ready.
================
*/
export function petRowCooldown( state: PetTraining, row: number, nowMs: number ): string {
	const r = state.rows[row];
	if ( !r ) return "";
	const ms = r.timerMs - Math.max( 0, nowMs - state.receivedAtMs );
	if ( ms > 0 ) return `${Math.ceil( ms / 1000 )}s`;
	return r.left > 0 ? `${r.left} ${r.kind.toLowerCase()}` : "";
}

/*
================
petSkillHelp

The help text of one skill slot: name, kind, level, effect now and next,
ranks, the next level's requirement and how to use it.
================
*/
export function petSkillHelp(
	design: PetSkillDesign,
	skill: PetTrainingSkill,
	petLevel: number,
	points: number
): string {
	const rules = PET_DESIGN.rules, lines = [ `${design.name} (${skill.action ? "action" : "passive"})` ];
	if ( !skill.live ) lines.push( "Planned skill. Unavailable to learn or use." );
	const bonus = skill.effectiveLevel > skill.level ? ` (+${skill.effectiveLevel - skill.level} from gear)` : "";
	lines.push( `Level ${skill.level} / ${design.maxLevel}${bonus}` );
	lines.push( `Tier ${design.option}: pet levels ${design.unlockLevel}–${design.tierEnd}.` );
	if ( design.option > 1 ) lines.push( `Requires previous tier at level ${design.maxLevel}.` );
	if ( skill.level > 0 ) lines.push( "Now: " + petEffect( design, skill.value, skill.effectiveLevel ) );
	else lines.push( "Effect: " + petEffect( design, skill.value ) );
	if ( skill.nextPetLevel > 0 ) {
		lines.push( "Next level: " + petEffect( design, skill.nextValue, skill.effectiveLevel + 1 ) );
		lines.push(
			`Needs pet level ${skill.nextPetLevel}` + (petLevel < skill.nextPetLevel ? ` (now ${petLevel})` : "") +
				` and 1 skill point (${points} free).`
		);
	} else lines.push( "Maximum level." );
	if ( design.cooldown !== null ) {
		const cooldown = Math.max(
			0,
			design.cooldown + (design.cooldownPerLevel ?? 0) * (Math.max( 1, skill.effectiveLevel ) - 1)
		);
		const shown = design.trigger === "Timer" ? cooldown : Math.max( 1, Math.floor( cooldown + 0.5 ) );
		lines.push( `Base recharge: ${formatValue( shown )} ${design.cooldownUnit} before gear bonuses.` );
		if ( skill.action ) lines.push( "Using this skill locks every active in this row." );
	}
	if ( design.family !== "Challenging Roar" ) {
		lines.push( `Proficiency ${rules.rank2Level}: ${design.rank2}` );
		lines.push( `Proficiency ${rules.rank3Level}: ${design.rank3}` );
	}
	if ( skill.action && skill.level > 0 ) {
		lines.push(
			skill.queued ?
				"Preparing this ability: the pet will use it when in range." :
				"Click or use its numbered quickslot to cast. Select an enemy for targeted abilities."
		);
	}
	if ( !skill.action && skill.level > 0 ) {
		lines.push( skill.slotted ? "Slotted: active in its row." : "Click to slot it (one passive per row)." );
	}
	return lines.join( "\n" );
}

/*
================
petSkillTooltip

Locked skills retain their help; unmet learning requirements use red.
================
*/
export function petSkillTooltip(
	design: PetSkillDesign,
	skill: PetTrainingSkill,
	petLevel: number,
	points: number,
	prerequisiteMet = true
): TooltipRow[] {
	const unmet = petLevel < skill.nextPetLevel || points < 1;
	return petSkillHelp( design, skill, petLevel, points ).split( "\n" ).map( value => ({
		value,
		color: value.startsWith( "Planned skill." ) ||
				(!prerequisiteMet && value.startsWith( "Requires previous tier" )) ||
				(unmet && value.startsWith( "Needs pet level" )) ?
			0xffff5555 :
			0xffffffff
	}) );
}

/*
================
petAffixLine

The tooltip line of one pet gear blue option, or null for any other
option (a native magic option renders through item-tooltip-magic).
================
*/
export function petAffixLine( encoded: string ): string | null {
	const bits = BigInt( encoded ), param = Number( bits & 0xffffn );
	if ( param <= PET_AFFIX_PARAM_BASE ) return null;
	const affix = PET_DESIGN.affixes.find( a => a.code === param - PET_AFFIX_PARAM_BASE );
	return affix ? affix.effect.replaceAll( "{v}", String( Number( bits >> 32n ) ) ) : null;
}
