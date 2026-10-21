/*
===========================================================================
fixture-state.ts - mutable synthetic inputs using the production field types

Fixtures intentionally publish partial sessions; the production UiView cast
stays at the frame's publication boundary. These types cover recipe mutations.
===========================================================================
*/
import type { UiView } from "../../src/engine/contracts/ui";
import type { GameplayState } from "../../src/engine/contracts/gameplay";
import type { EntityState } from "../../src/engine/contracts/world";

/*
================
Mutable
================
*/
export type Mutable<T> = T extends string | number | boolean ? T :
	T extends ReadonlyMap<unknown, unknown> ? T :
	T extends ( ...args: never[] ) => unknown ? T :
	T extends readonly unknown[] ? { [K in keyof T]: Mutable<T[K]>; } :
	T extends object ? { -readonly [K in keyof T]: Mutable<T[K]>; } :
	T;

/*
================
FixtureGameplay

Every world seed supplies these owners. Other publications are optional until
the corresponding recipe installs them; partial nested records remain explicit.
================
*/
type SeededOwners =
	| "localGid"
	| "pose"
	| "target"
	| "inventory"
	| "vitals"
	| "cosRecords"
	| "social"
	| "stall"
	| "progression"
	| "chat";
export type FixtureGameplay =
	& Mutable<GameplayState>
	& {
		[K in SeededOwners]-?: Mutable<NonNullable<GameplayState[K]>>;
	};

/*
================
FixtureState
================
*/
export type FixtureState = Mutable<Omit<Partial<UiView>, "gameplay" | "entities" | "session" | "frontend">> & {
	gameplay: FixtureGameplay;
	session: Mutable<NonNullable<UiView["session"]>>;
	frontend: Mutable<Partial<NonNullable<UiView["frontend"]>>>;
	entities: Mutable<EntityState>[];
};

/*
================
FrontendFixtureState

The frontend owners clear gameplay when leaving the synthetic world.
================
*/
export type FrontendFixtureState = Omit<FixtureState, "gameplay"> & { gameplay: FixtureGameplay | null; };
