/*
===========================================================================
ui-entry-recipes.d.mts - type boundary for shared synthetic fixture recipes
===========================================================================
*/
import type { UiEvent } from "../../src/engine/contracts/ui";
import type { Mutable } from "./fixture-state";
import type { GameplayState } from "../../src/engine/contracts/gameplay";
import type { EntityState } from "../../src/engine/contracts/world";

/** Partial mutable snapshots intentionally match the existing runtime fixture. */
export interface EntryFixture {
	state: {
		gameplay: Mutable<Partial<GameplayState>>;
		entities: Mutable<Partial<EntityState> & Pick<EntityState, "gid">>[];
	};
	ui: { event( event: UiEvent ): void; };
}
export type EntryRecipe = ( fixture: EntryFixture, step: () => void | Promise<void> ) => void | Promise<void>;
export const OPENERS: Record<string, EntryRecipe>;
export const SERVICE_WINDOWS: Record<string, EntryRecipe>;
