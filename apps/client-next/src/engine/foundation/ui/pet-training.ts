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
import { petGear } from "./pet-gear";
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
	training: PetTraining | undefined,
	resolve?: { readonly stacks: number; readonly tier: number; readonly untilMs: number; } | null,
	nowMs = 0,
	level = 0
): CosReference | undefined {
	if ( !reference ) return reference;
	const activeResolve = resolve && resolve.untilMs > nowMs ?
		Math.min( 10, Math.max( 0, resolve.stacks ) ) :
		resolve === undefined ?
		Math.min( 10, Math.max( 0, training?.counters?.resolve ?? 0 ) ) :
		0;
	if ( !training?.stats ) {
		if ( !activeResolve || !resolve?.tier ) return reference;
		const tierBook = PET_DESIGN.treeSkills.find(
			s => s.family === "Unbroken Spirit" && s.option === resolve.tier
		);
		const perStack = tierBook?.value ?? 0;
		const bearPct = PET_DESIGN.pets.find( p => p.name === "Bear" )?.hpPercent ?? 100;
		const baseMaxHp = profileHealth( "Bear", level, reference.maxHp * bearPct / 100 );
		return perStack > 0 ?
			{ ...reference, maxHp: Math.trunc( baseMaxHp * (1 + activeResolve * perStack / 100) ) } :
			reference;
	}
	const spirit = training.skills.find(
		s => s.slotted && s.level > 0 && petSkillDesign( s.id )?.family === "Unbroken Spirit"
	);
	const species = petPetName( training );
	const speciesPct = PET_DESIGN.pets.find( p => p.name === species )?.hpPercent ?? 100;
	const gearHP = training.gear.reduce( ( sum, item ) => {
		const gear = petGear( item?.refObjId );
		return sum + (gear && (!level || gear.degree.minPetLevel <= level) ? gear.item.baseStats.maxHPPercent : 0);
	}, 0 );
	const unbuffedMaxHp = Math.trunc(
		profileHealth( species, level, reference.maxHp * speciesPct / 100 ) * (1 + Math.min( 2250, gearHP ) / 100)
	);
	let maxHp = training.stats.maxHp;
	if ( spirit && activeResolve > 0 && (maxHp === unbuffedMaxHp || maxHp === reference.maxHp) ) {
		maxHp = Math.trunc( maxHp * (1 + activeResolve * spirit.value / 100) );
	} else if ( spirit && resolve && resolve.untilMs <= nowMs && maxHp > unbuffedMaxHp ) {
		maxHp = unbuffedMaxHp;
	}
	return {
		...reference,
		maxHp,
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
profileHealth

The server's authored curve; keep native references unchanged when no level is known.
================
*/
function profileHealth( species: string, level: number, fallback: number ): number {
	const curve = PET_DESIGN.pets.find( p => p.name === species )?.healthByLevel;
	if ( !level || !curve?.length ) return Math.trunc( fallback );
	let previous: { readonly level: number; readonly hp: number; } = curve[0]!;
	for ( const point of curve.slice( 1 ) ) {
		if ( level <= point.level ) {
			return previous.hp +
				Math.trunc( (point.hp - previous.hp) * (level - previous.level) / (point.level - previous.level) );
		}
		previous = point;
	}
	return previous.hp;
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
export const PET_GEAR_SLOTS = [
	{ name: "Collar", socket: 10 },
	{ name: "Harness", socket: 1 },
	{ name: "Charm", socket: 11 }
] as const;

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
petRoarRadius

Matches companion.SkillDef.RoarRadiusMeters; includes effective skill bonuses.
================
*/
export function petRoarRadius( tier: number, level: number ): number {
	const steps = Math.max( 1, level ) - 1;
	return tier === 1 ? 10 + steps : tier === 3 ? 30 + steps : 12 + steps * 0.4;
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
wolfSkillEffectLines

The four reworked rows share concise full-help and character-style tooltips.
================
*/
function wolfSkillEffectLines( d: PetSkillDesign, value: number ): string[] | null {
	const v = formatValue( value );
	switch ( d.family ) {
		case "Hunting Howl":
			return [
				`Attack speed +${v}%`,
				"Duration 8 s",
				d.option === 1 ? "Wolf only" : d.option === 2 ? "Wolf and owner" : "Wolf, owner and party within 20 m",
				"No target required; unavailable in town"
			];
		case "Savage Rend":
			return [
				`Physical damage ${v}%`,
				`Bleeding: ${d.option >= 2 ? 4 : 2} x pet level every 2 s`,
				"Bleeding duration 6 s",
				...(d.option >= 2 ?
					[ `Maximum targets ${d.option === 3 ? 5 : 3}`, `Cleave range ${d.option === 3 ? 6 : 4} m` ] :
					[]),
				...(d.option === 3 ? [ "Direct damage +25% against bleeding enemies" ] : [])
			];
		case "Bloodscent":
			return [
				`Damage +${v}% per attack stack`,
				`Maximum stacks ${d.option === 1 ? 5 : d.option === 2 ? 8 : 10}`,
				"Duration 6 s; attacks refresh",
				...(d.option >= 2 ? [ "Separate wolf and owner stacks" ] : [ "Wolf only" ]),
				...(d.option === 3 ? [ "Critical chance +0.5% per stack" ] : [])
			];
		case "Blood Hunt":
			return [
				"Four hits mark one enemy",
				`Savage Rend bonus damage +${v}%`,
				"Mark duration 8 s; hits refresh",
				d.option === 1 ? "Wolf hits only" : "Wolf and owner share charges",
				"Switching prey resets charges",
				...(d.option === 3 ? [ "Burst: 50% of bonus damage", "Up to 5 other bleeding enemies within 6 m" ] : [])
			];
		case "Ice Lance":
			return [
				`Magical damage ${v}%`,
				"Casting range 15 m",
				"Frostbite chance 40%",
				...(d.option >= 2 ? [ "Pierces 2 additional enemies" ] : []),
				...(d.option === 3 ? [ "Frostbitten targets: damage +50%" ] : [])
			];
		case "Blizzard":
			return [
				`Magical damage ${v}% per pulse`,
				"4 pulses, one second apart",
				`Radius ${d.option >= 2 ? 7 : 5} m; maximum targets 8`,
				...(d.option >= 2 ? [ "Applies frostbite slow" ] : []),
				...(d.option === 3 ? [ "Freezes enemies hit by all four pulses" ] : []),
				"Native resistance applies"
			];
		case "Shatter":
			return [
				`Damage to chilled enemies +${v}%`,
				d.option >= 2 ? "Penguin and owner" : "Penguin only",
				...(d.option === 3 ?
					[ "Frozen kills burst within 5 m", "50% of killing damage; maximum targets 5" ] :
					[])
			];
		case "Arcane Resonance":
			return [
				`Magical attack +${v}% per stack`,
				"Maximum stacks 5; duration 8 s",
				...(d.option >= 2 ? [ "Owner magic hits build separate stacks" ] : []),
				...(d.option === 3 ?
					[
						"At 5 penguin stacks: next active repeats once",
						"Consumes penguin stacks; echoes do not build charges"
					] :
					[])
			];
		case "Frozen Hourglass":
			return [
				"Penguin spell hits build charges",
				...(d.option >= 2 ? [ "Owner magic hits also build charges" ] : []),
				"Maximum charges 5; once per action",
				"At 5 charges: next active recovers 2 s",
				"Shared charges; no charges from echoes or storm pulses",
				...(d.option === 3 ? [ "Empowered frostbite and storm slow last 2 s longer" ] : [])
			];
		default:
			return null;
	}
}

/*
================
petEffect

A skill's effect text at a value ({v} substituted).
================
*/
export function petEffect( design: PetSkillDesign, value: number, effectiveLevel = 1 ): string {
	const wolf = wolfSkillEffectLines( design, value );
	if ( wolf ) return wolf.join( ". " ) + ".";
	const rank2 = effectiveLevel >= PET_DESIGN.rules.rank2Level || design.option > 1;
	const rank3 = effectiveLevel >= PET_DESIGN.rules.rank3Level || design.option > 1;
	if ( design.family === "Rejuvenating Plume" ) {
		return `Restore ${formatValue( value )}% maximum HP: half immediately, half over 5 seconds.` +
			(design.option >= 2 ? " Also heals the crow and removes one poison or burn from the owner." : "") +
			(design.option === 3 ? " Party range 20 m; each additional recipient receives 20% less healing." : "");
	}

	if ( design.family === "Stormbrood" ) {
		return "Destructible egg hatches after 30 s into an unhealable crow. Clone lasts 10 min; stats and normal attacks only. Maximum 10 eggs/clones." +
			(design.option >= 2 ? " Stores 3 casts." : "") +
			(design.option === 3 ? " Clone death: 8 m wind burst, 500% normal attack damage; up to 8 monsters." : "");
	}

	if ( design.family === "Unbroken Spirit" ) {
		return `Landed monster hits grant Resolve for 15 s, up to 10 stacks. Each stack adds ${
			formatValue( value )
		}% maximum HP and fills only the added HP.` +
			" Further hits refresh the duration. Blocks and periodic damage do not count; shielded hits count." +
			(design.option >= 2 ? " At 10 stacks, resist new slows and knockback." : "") +
			(design.option === 3 ?
				" At 10 stacks, your next successful Roar gains 50% radius and target capacity, or Iron Hide gains 50% shield capacity; all stacks are consumed." :
				"") +
			" Expiry, consumption or leaving combat eligibility removes the bonus HP without killing the bear.";
	}
	if ( design.family === "Crushing Paw" ) {
		return `Every 10 landed physical or spell hits taken by the bear stuns its attacker for ${
			formatValue( value )
		} s.` +
			(design.option >= 2 ? " The owner has a separate 10-hit counter." : "") +
			(design.option === 3 ?
				" Other monsters within 8 m of the hit recipient each have a 50% chance to be stunned." :
				"") +
			" Blocked hits and periodic damage do not count. Shielded hits count. Unique monsters are immune.";
	}

	if ( design.family === "Guiding Feathers" ) {
		return `Crow hits grant +${formatValue( value )}% hit ratio per Precision stack for 10 s, up to 5.` +
			(design.option >= 2 ?
				` At 5 stacks, your next damaging skill deals ${
					20 + (effectiveLevel - 1) * 2
				}% more damage and consumes Precision.` :
				"") +
			(design.option === 3 ? " Party members within 20 m gain independent stacks." : "");
	}

	if ( design.family === "Challenging Roar" ) {
		const radius = petRoarRadius( design.option, effectiveLevel );
		return `Taunts up to ${design.option === 3 ? 6 : 3} monsters within ${formatValue( radius )} m. ` +
			(design.option >= 2 ? "Party monsters allowed. " : "Party monsters require Tier II. ") +
			"Unique monsters are immune. Taunt applies after a 1 s cast. " +
			"Can cast without targets. Cannot cast in town. No damage.";
	}
	if ( design.family === "Iron Hide" ) {
		return `Shields the bear and its owner for ${formatValue( Math.min( 100, value ) )}% of maximum HP for 5 s.` +
			(design.option >= 2 ? " Also shields party members within 20 m." : "") +
			(design.option === 3 ?
				" After 5 s, each recipient's remaining shield (including the bear) explodes within 8 m for 50% of its remaining value as damage to monsters." :
				"") +
			" Cannot cast in town.";
	}
	if ( design.family === "Guardian's Bond" ) {
		const rank = Math.min( 10, effectiveLevel );
		if ( design.option === 1 ) {
			return `Revives the owner with ${rank * 10}% HP and ${rank * 10}% lost EXP. ` +
				`Shared revival cooldown: ${formatValue( 600 - (rank - 1) * 300 / 9 )} s.`;
		}
		const transfer = (design.option === 2 ? 10 : 20) + (rank - 1) * 10 / 9;
		return `Transfers ${formatValue( transfer )}% of ${
			design.option === 3 ? "owner/party" : "owner"
		} HP damage to the bear.`;
	}
	return design.effect.replaceAll( "{v}", formatValue( value ) );
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
	if ( !cooldown || cooldown.timerMs <= 0 || cooldown.kind === "Charges" && cooldown.left > 0 ) return [];
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

// A missing training state is asked for again this often, this many times.
const PET_STATE_RETRY_MS = 3000, PET_STATE_RETRIES = 3;

export type PetStateAsk = { readonly ref: number; readonly asks: number; readonly atMs: number; };

/*
================
petStateAsk

Whether the quickslot bar asks for the attack pet's 0x3FB1 state now. A
new pet is asked at once. While its state is missing (asked too early, or
cleared by a world re-entry) it is asked again a few times, spaced out; a
received state restores the retries. Returns the next ask record.
================
*/
export function petStateAsk(
	last: PetStateAsk,
	pet: { readonly refObjId: number; readonly gid: number; } | undefined,
	stateGid: number | undefined,
	nowMs: number
): { ask: boolean; next: PetStateAsk; } {
	if ( !pet ) return { ask: false, next: { ref: 0, asks: 0, atMs: 0 } };
	if ( pet.refObjId !== last.ref ) return { ask: true, next: { ref: pet.refObjId, asks: 1, atMs: nowMs } };
	if ( stateGid === pet.gid ) return { ask: false, next: { ...last, asks: 0 } };
	if ( last.asks >= PET_STATE_RETRIES || nowMs - last.atMs < PET_STATE_RETRY_MS ) return { ask: false, next: last };
	return { ask: true, next: { ref: last.ref, asks: last.asks + 1, atMs: nowMs } };
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
	if ( r.kind === "Charges" && r.left > 0 ) return "";
	if ( ms > 0 ) return `${Math.ceil( ms / 1000 )}s`;
	return r.left > 0 ? `${r.left} ${r.kind.toLowerCase()}` : "";
}

/*
================
petSkillHelp

Accessible text mirrors the compact native-style tooltip. Controls live in row help.
================
*/
export function petSkillHelp(
	design: PetSkillDesign,
	skill: PetTrainingSkill,
	petLevel: number,
	points: number,
	prerequisiteMet = true
): string {
	return petSkillTooltip( design, skill, petLevel, points, prerequisiteMet ).map( row => row.value ).join( "\n" );
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
	const white = 0xffffffff, gold = 0xffefdaa4, red = 0xffff5555;
	const rows: TooltipRow[] = [ { value: design.name, color: white } ];
	const add = ( value: string, color = gold, heading = false ) =>
		rows.push( {
			value,
			color,
			...(heading ? { heading: true, ornament: "diamond" as const } : {})
		} );
	add( skill.action ? "Active" : "Passive" );
	add( "Effect", gold, true );
	for ( const value of petSkillEffectLines( design, skill.value, Math.max( 1, skill.effectiveLevel ) ) ) {
		add( value, white );
	}
	if ( skill.effectiveLevel > skill.level ) add( `Skills +${skill.effectiveLevel - skill.level}`, white );
	if ( design.cooldown !== null ) {
		const recharge = Math.max(
			0,
			design.cooldown +
				(design.cooldownPerLevel ?? 0) *
					(Math.max(
						1,
						design.family === "Stormbrood" ?
							Math.min( 10, skill.effectiveLevel ) :
							skill.effectiveLevel
					) - 1)
		);
		add(
			`Base recharge: ${
				formatValue( design.trigger === "Timer" ? recharge : Math.max( 1, Math.round( recharge ) ) )
			} ${design.cooldownUnit}`,
			white
		);
	}
	if ( skill.nextPetLevel > 0 ) {
		const current = petSkillEffectLines( design, skill.value, Math.max( 1, skill.effectiveLevel ) );
		const next = petSkillEffectLines( design, skill.nextValue, skill.level ? skill.effectiveLevel + 1 : 1 );
		const changes = skill.level ? next.filter( value => !current.includes( value ) ) : [];
		if ( changes.length ) {
			add( "Next", gold, true );
			for ( const value of changes ) add( value, white );
		}
		add( "Required", gold, true );
		add( `Pet level ${skill.nextPetLevel}`, petLevel < skill.nextPetLevel ? red : white );
		add( "Skill points 1", points < 1 ? red : white );
		if ( design.option > 1 ) {
			const previous = petSkillDesign( design.id - 1 );
			add( `${previous?.name ?? "Previous book"} ${design.maxLevel}`, prerequisiteMet ? white : red );
		}
	}
	if ( !skill.live ) add( "Unavailable", red );
	return rows;
}

/*
================
petSkillEffectLines

Short native-style effect rows. General controls and combat rules live in row help.
================
*/
function petSkillEffectLines( d: PetSkillDesign, value: number, rank: number ): string[] {
	const wolf = wolfSkillEffectLines( d, value );
	if ( wolf ) return wolf;
	const v = formatValue( value ),
		advanced = d.option > 1 || rank >= PET_DESIGN.rules.rank2Level,
		mastered = d.option > 1 || rank >= PET_DESIGN.rules.rank3Level;
	switch ( d.family ) {
		case "Execute":
			return [
				d.option > 1 ? "Wolf and owner" : "Wolf only",
				...(d.option === 3 ? [ "Every 7 attacks: execution sweep", "Sweep range 10 m around attacker" ] : []),
				`Execute enemies below ${v}% HP`,
				...(advanced ? [ "Bleeding targets: threshold +5%" ] : []),
				"Requires a damaging hit; uniques immune"
			];

		case "Rejuvenating Plume":
			return [
				`Restore ${v}% maximum HP`,
				"Half immediately; half over 5 s",
				...(d.option >= 2 ? [ "Also heals the crow", "Removes one poison or burn from owner" ] : []),
				...(d.option === 3 ? [ "Party range 20 m", "Each additional recipient: 20% less healing" ] : [])
			];
		case "Stormbrood":
			return [
				"Egg hatches after 30 s",
				"Clone lasts 10 min and cannot be healed",
				"Copies stats and normal attacks only",
				...(d.option >= 2 ? [ "3 stored casts" ] : []),
				"Maximum 10 eggs/clones",
				...(d.option === 3 ? [ "On death: 500% attack wind burst", "Radius 8 m; up to 8 monsters" ] : [])
			];

		case "Mana Spring":
			return [
				`Restore MP equal to ${v}% of bird damage`,
				d.option >= 2 ? "Owner and party members within 20 m" : "Owner only",
				...(d.option === 3 ? [ "Each damaging attack: character/pet reuse -0.5 s" ] : []),
				"Uses actual damage; blocked hits grant nothing"
			];
		case "Tailwind":
			return [
				`Movement speed +${v}% per stack`,
				"Gain 1 stack every 5 s; maximum 10",
				"Dealing or taking damage removes all stacks",
				...(d.option >= 2 ?
					[
						"At 10 stacks: next hit dealt or taken stuns that enemy for 2 s",
						"Native resistance and unique immunity apply"
					] :
					[]),
				...(d.option === 3 ? [ "Party members within 20 m gain independent stacks" ] : [])
			];
		case "Guiding Feathers":
			return [
				`Hit ratio +${v}% per Precision stack`,
				"Crow hits grant 1 stack; maximum 5",
				"Duration 10 s; crow hits refresh duration",
				...(d.option >= 2 ?
					[
						`At 5 stacks: next damaging skill +${20 + (rank - 1) * 2}% damage`,
						"Successful skill consumes your Precision",
						"Normal attacks do not consume stacks"
					] :
					[]),
				...(d.option === 3 ? [ "Party range 20 m; independent counters" ] : [])
			];

		case "Challenging Roar":
			return [
				`Taunt ${d.option === 3 ? 6 : 3} targets`,
				`Range ${formatValue( petRoarRadius( d.option, rank ) )} m`,
				"Casting time 1 s",
				...(d.option > 1 ? [ "Includes party monsters" ] : [])
			];
		case "Iron Hide":
			return [
				`Shield ${formatValue( Math.min( 100, value ) )}% maximum HP`,
				"Duration 5 s",
				d.option > 1 ? "Bear, owner and party within 20 m" : "Bear and owner",
				...(d.option === 3 ?
					[ "Expiry: remaining shield bursts", "Damage 50% remaining shield", "Burst range 8 m" ] :
					[])
			];
		case "Unbroken Spirit":
			return [
				`Maximum HP +${v}% per stack`,
				"Maximum Resolve 10",
				"Duration 15 s; renewed on hit",
				...(d.option > 1 ? [ "Full stacks: slow/knockback resist" ] : []),
				...(d.option === 3 ?
					[
						"Next Roar: range/targets +50%",
						"Or next shield: capacity +50%",
						"Consumes full Resolve"
					] :
					[])
			];
		case "Crushing Paw":
			return [
				"Every 10 hits taken: stun attacker",
				`Stun duration ${v} s`,
				...(d.option > 1 ? [ "Bear and owner count separately" ] : []),
				...(d.option === 3 ? [ "Nearby enemies: 50% stun chance", "Range 8 m around the hit recipient" ] : [])
			];
		case "Guardian's Bond": {
			const r = Math.min( 10, rank ), recovery = d.option === 1 ? r * 10 : 100;
			return [
				d.option === 3 ? "Auto-revive owner and party" : "Auto-revive owner",
				`HP and lost EXP restored ${recovery}%`,
				`Revival recharge ${formatValue( d.option === 1 ? 600 - (r - 1) * 300 / 9 : 300 )} s`,
				"Range 12 m",
				"Revival limit: pet level +10",
				...(d.option > 1 ?
					[
						`Damage shared with bear ${formatValue( (d.option === 2 ? 10 : 20) + (r - 1) * 10 / 9 )}%`,
						"Transfer cap 20% bear HP/s; leaves 1 HP"
					] :
					[])
			];
		}
		default:
			return petEffect( d, value, rank ).replace( /On command, /g, "" ).replace( /Commanded strike /g, "" )
				.replace( /Party monsters require Tier II\. /g, "" ).split( /(?<=\.)\s+/ ).filter( line =>
					!line.startsWith( "No separate" )
				).map( line => line.replace( /\.$/, "" ) );
	}
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
