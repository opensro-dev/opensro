/*
===========================================================================

union-hud.ts - the union tab's open question

CIFAllianceGuild_AskExitOrExpel (5F7670) raises one question box: leave
the union (UIIT_MSG_GUILD_QUESTION_ALLY_EXIT) or expel the selected
guild (UIIT_MSG_QUESTION_GUILD_RESPECT_ALLY_EXPEL with its name). A yes
sends the request (5F5690); either answer closes the box. The UI draws
from this owner every frame, including the guild list's order (the
name and level sort buttons, ids 63 and 64).

===========================================================================
*/

/*
================
UnionAsk
================
*/
export interface UnionAsk {
	readonly kind: "exit" | "expel";
	readonly guild: number;
	readonly name: string;
}

/*
================
createUnionHud
================
*/
export function createUnionHud() {
	let ask: UnionAsk | null = null;
	let sort: "name" | "level" = "name", descending = false;
	return {
		/*
		================
		sortBy

		A second press of the same button reverses the order.
		================
		*/
		sortBy( key: "name" | "level" ) {
			descending = key === sort ? !descending : false;
			sort = key;
		},
		/*
		================
		order
		================
		*/
		order<T extends { readonly name: string; readonly level: number; }>( rows: readonly T[] ): T[] {
			const sign = descending ? -1 : 1;
			return [ ...rows ].sort( ( a, b ) =>
				sign *
				(sort === "level" ?
					a.level - b.level || a.name.localeCompare( b.name ) :
					a.name.localeCompare( b.name ))
			);
		},
		/*
		================
		ask

		5F7670 keeps one box: a second ask while one is up is ignored.
		================
		*/
		ask( next: UnionAsk ) {
			if ( !ask ) ask = next;
		},
		/*
		================
		question
		================
		*/
		question(): UnionAsk | null {
			return ask;
		},
		/*
		================
		answer

		Closes the box and returns what a yes asked for.
		================
		*/
		answer(): UnionAsk | null {
			const asked = ask;
			ask = null;
			return asked;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			ask = null;
		}
	};
}
