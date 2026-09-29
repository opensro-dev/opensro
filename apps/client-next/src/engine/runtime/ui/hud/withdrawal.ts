/*
===========================================================================

withdrawal.ts - restoration dialog selection and advisory quotes

Inventory remains authoritative. Opening or closing the dialog never spends
anything. Quotes use current learned ranks, including both dependency slots;
the server repeats all checks when the player confirms.

===========================================================================
*/
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { WithdrawalCommand } from "@/engine/foundation/gameplay/withdrawal";

const BAG_START = 13;
// 58DEE0 resizes control 5 (the outer skill frame), not control 10 (the board).
export const WITHDRAWAL_SKILL_FRAME_HEIGHT = 305;
// 5DE8A0 mode 3 resizes the authored box and moves its two action buttons.
export const WITHDRAWAL_CONFIRM_SIZE = [ 300, 208 ] as const;
export const WITHDRAWAL_CONFIRM_BUTTON_Y = 167;
export const WITHDRAWAL_CONFIRM_FILL_HEIGHT = 152;
export const RESUSCITATION_POTION_ID = 3673;
export const RESUSCITATION_CONFIRM_SIZE = [ 300, 244 ] as const;
export const RESUSCITATION_CONFIRM_BUTTON_Y = 212;
export const RESUSCITATION_CONFIRM_FILL_HEIGHT = 188;
const REFUND_LOSS_DIVISOR = 5;
const GOLD_REFUND_FRACTION = 0.8;
const GOLD_MULTIPLIER = 80;
const RANKS_PER_GOLD_STEP = 10;

/*
================
WithdrawalChoice
================
*/
export interface WithdrawalChoice {
	readonly id: number;
	readonly kind: WithdrawalCommand["kind"];
	readonly rank: number;
	readonly level: number;
	readonly minimum: number;
	readonly name: string;
	readonly nameSymbol?: string;
	readonly refund: number;
	readonly blocked: boolean;
}

/*
================
createWithdrawalDialog

The native skill pane stays open behind its rank-removal confirmation.
5DF030 starts the removable-rank spinner at zero and caps it by dependencies
and available potions. No optimistic mutation or automatic retry spends items.
================
*/
export function createWithdrawalDialog() {
	let potion = 0, selected = "", amount = 0;
	let observedRevision = 0;
	return {
		/*
		================
		observe

		Only a new authoritative NPC response opens the quest-potion window.
		Closing it cannot be undone by the next snapshot of the same receipt.
		================
		*/
		observe( revision: number ) {
			if ( revision === observedRevision ) return false;
			observedRevision = revision;
			potion = revision ? RESUSCITATION_POTION_ID : 0;
			selected = "";
			amount = 0;
			return true;
		},
		/*
		================
		open
		================
		*/
		open( id: number ) {
			potion = id;
			amount = 0;
			selected = "";
		},
		/*
		================
		close
		================
		*/
		close() {
			potion = 0;
			amount = 0;
			selected = "";
		},
		/*
		================
		active
		================
		*/
		active() {
			return potion !== 0;
		},
		/*
		================
		boundToNpc
		================
		*/
		boundToNpc() {
			return potion === RESUSCITATION_POTION_ID;
		},
		/*
		================
		select
		================
		*/
		select( key: string ) {
			selected = key;
			amount = 0;
		},
		/*
		================
		adjust
		================
		*/
		adjust( delta: number ) {
			amount = Math.max( 0, amount + delta );
		},
		/*
		================
		confirming
		================
		*/
		confirming() {
			return selected !== "";
		},
		/*
		================
		read

		Read current inventory and ranks each time; an incoming receipt or item
		move can invalidate a selection while the dialog remains visible.
		================
		*/
		read(
			game: GameplayState,
			costs: Readonly<Record<number, number>>,
			goldPrices: Readonly<Record<number, number>> = {}
		) {
			const catalog = new Map( game.skillCatalog?.map( row => [ row.id, row ] ) );
			const learned = (game.skills ?? []).map( id => catalog.get( id ) ).filter( row => row !== undefined );
			const choices: WithdrawalChoice[] = [];
			for ( const row of learned ) {
				if ( !row.trainable || row.spCost <= 0 ) continue;
				const rank = row.level - 1;
				let minimum = 0;
				for ( const other of learned ) {
					if ( other.id === row.id ) continue;
					for ( const requirement of other.prerequisites ) {
						if ( requirement.ID === row.group ) minimum = Math.max( minimum, 1, requirement.Level );
					}
				}
				choices.push( {
					id: row.id,
					kind: "skill-withdraw",
					rank,
					level: row.level,
					minimum,
					name: row.name,
					nameSymbol: row.nameSymbol,
					refund: row.spCost,
					blocked: learned.some( other =>
						other.id !== row.id && other.prerequisites.some(
							requirement => requirement.ID === row.group && (rank === 0 || requirement.Level > rank)
						)
					)
				} );
			}
			for ( const row of game.progression?.masteries ?? [] ) {
				if ( row.level <= 0 ) continue;
				const rank = row.level - 1, refund = rank === 0 ? 0 : costs[rank];
				let minimum = 0;
				for ( const skill of learned ) {
					for ( const requirement of skill.masteries ) {
						if ( requirement.ID === row.id ) minimum = Math.max( minimum, requirement.Level );
					}
				}
				choices.push( {
					id: row.id,
					kind: "mastery-withdraw",
					rank,
					level: row.level,
					minimum,
					name: `Mastery ${row.id}`,
					refund: refund ?? 0,
					blocked: refund === undefined || learned.some( skill =>
						skill.masteries.some(
							requirement => requirement.ID === row.id && requirement.Level > rank
						)
					)
				} );
			}
			const quantity = game.inventory.filter( item => item.slot >= BAG_START && item.refObjId === potion )
				.reduce( ( total, item ) => total + item.quantity, 0 );
			const source = choices.find( row => `${row.kind}:${row.id}` === selected );
			const maximum = source ? Math.min( quantity, source.level - source.minimum ) : 0;
			amount = Math.min( amount, Math.max( 0, maximum ) );
			const resuscitation = potion === RESUSCITATION_POTION_ID;
			const basis = goldPrices[game.progression?.level ?? 0];
			let refund = 0, gold = 0, valid = !resuscitation || basis !== undefined;
			if ( source ) {
				for ( let rank = source.level; rank > source.level - amount; rank-- ) {
					const removed = source.kind === "skill-withdraw" ?
						game.skillCatalog?.find( row =>
							row.trainable && row.group === catalog.get( source.id )?.group && row.level === rank
						) :
						undefined;
					const cost = source.kind === "mastery-withdraw" ?
						(rank === 1 ? 0 : costs[rank - 1]) :
						removed?.spCost;
					if ( cost === undefined ) valid = false;
					else refund += cost;
					if ( resuscitation && basis !== undefined ) {
						const goldRank = source.kind === "mastery-withdraw" ?
							rank - 1 :
							removed?.masteries.find( requirement => requirement.ID !== 0 )?.Level ?? 0;
						const scale = basis / GOLD_REFUND_FRACTION * GOLD_MULTIPLIER;
						gold += Math.floor( (1 + goldRank / RANKS_PER_GOLD_STEP) * scale + 0.5 );
					}
				}
			}
			if ( resuscitation ) refund -= Math.floor( refund / REFUND_LOSS_DIVISOR );
			const choice = source ? { ...source, rank: source.level - amount, refund } : undefined;
			const command: WithdrawalCommand | null =
				choice && valid && gold <= Number( game.progression?.gold ?? 0 ) && amount > 0 && !choice.blocked &&
					quantity > 0 && !game.trainingPending &&
					!game.inventoryPending ?
					{ kind: choice.kind, id: choice.id, rank: choice.rank, potion } :
					null;
			return {
				rows: choices,
				selected,
				choice,
				command,
				quantity,
				gold,
				resuscitation,
				potion: game.inventory.find( item => item.refObjId === potion ),
				amount,
				maximum
			};
		}
	};
}
