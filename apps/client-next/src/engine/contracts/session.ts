/*
===========================================================================

session.ts - the title session contract between the worker and the UI

Server rows, character roster records (worn and avatar items as native
(RefItemID, plus) pairs) and the session commands and states.

===========================================================================
*/
export interface CharacterDraft {
	readonly characterName: string;
	readonly modelCodename: string;
	readonly heightIndex: number;
	readonly volumeIndex: number;
	readonly weaponIndex: number;
	readonly protectorIndex: number;
	readonly armorSelected: boolean;
	readonly weaponSelected: boolean;
}
export type CharacterOperationCommand =
	| { readonly kind: "check-name"; readonly operationId: number; readonly characterName: string; }
	| { readonly kind: "create-character"; readonly operationId: number; readonly draft: CharacterDraft; }
	| { readonly kind: "delete-character"; readonly operationId: number; readonly characterName: string; }
	| { readonly kind: "restore-character"; readonly operationId: number; readonly characterName: string; }
	| { readonly kind: "cancel-character-operation"; };
export interface CharacterOperationResult {
	readonly operationId: number;
	readonly kind: "check-name" | "create-character" | "delete-character" | "restore-character";
	readonly status: "pending" | "succeeded" | "failed";
	readonly nativeErrorCode?: number;
	readonly error?: string;
}
export interface LoginCommand {
	readonly kind: "login";
	readonly apiBase: string;
	readonly id: string;
	readonly password: string;
	readonly serverId: string;
	readonly divisionId?: string;
}
export type WorldExitCommand = { readonly kind: "exit"; };
export type SessionCommand =
	| WorldExitCommand
	| { readonly kind: "chat-blocks"; readonly value: readonly string[]; }
	| {
		readonly kind: "game-options";
		readonly value: import("@/engine/foundation/gameplay/game-options").GameOptions;
	}
	| CharacterOperationCommand
	| { readonly kind: "gameplay"; readonly command: import("./gameplay").GameplayCommand; }
	| { readonly kind: "enter-world"; readonly character: string; }
	| { readonly kind: "disconnect"; }
	| { readonly kind: "restart"; }
	| { readonly kind: "reconnect"; }
	| { readonly kind: "world-ready"; readonly travelRevision?: number; }
	| LoginCommand
	| {
		readonly kind: "logout";
	}
	| {
		readonly kind: "servers";
		readonly apiBase: string;
	}
	| {
		readonly kind: "roster";
	};
export type SessionState = Readonly<{
	disconnectMessage?: string;
	incidentID?: string;
	incidentDelivery?: "pending" | "sent" | "failed";
	phase:
		| "signed-out"
		| "listing-servers"
		| "loading-roster"
		| "authenticating"
		| "character-select"
		| "failed"
		| "connecting"
		| "entering-world"
		| "world"
		| "reconnecting"
		| "disconnected";
	revision: number;
	characterOperation?: CharacterOperationResult;
	character?: string;
	restoringWorld?: boolean;
	entityCount?: number;
	crestPrefix?: number;
	nativeServerName?: string;
	// A server answered that it speaks another release protocol (426): this
	// page must be refreshed into the newer release.
	releaseOutdated?: boolean;
	marksBase?: string;
	divisionId?: string;
	error?: string;
	code?: string;
	nativeTitleStatus?: number;
	nativeTitleArgument?: number;
	servers?: readonly ServerRecord[];
	characters?: readonly CharacterRecord[];
}>;
export interface SessionOwner {
	isWorldReady(): boolean;
	command( command: SessionCommand ): void;
	step( now?: number ): SessionState | null;
	takeWorld(): import("./world").WorldBatch | null;
	ackWorld( sequence: number ): void;
	dispose(): void;
}
export interface ServerRecord {
	readonly id: string;
	readonly name: string;
	readonly onlinePlayers: number;
	readonly capacity: number;
	readonly nativeServerId: number;
	readonly nativeFarmId: number;
	readonly isTest: boolean;
	readonly operating: boolean;
	readonly transportUrl: string;
}
// One worn or avatar item of a character-list row: native (RefItemID, plus).
export interface CharacterItem {
	readonly refObjId: number;
	readonly plus: number;
}
export interface CharacterRecord {
	readonly id: number;
	readonly name: string;
	readonly level: number;
	readonly raceIndex: number;
	readonly gender: number;
	readonly figureIndex: number;
	readonly heightIndex: number;
	readonly volumeIndex: number;
	readonly weaponIndex: number;
	readonly protectorIndex: number;
	readonly armorSelected: boolean;
	readonly weaponSelected: boolean;
	readonly deletePending: boolean;
	readonly deletionBlocker?: "guild-master" | "guild-member" | "academy-guardian" | "academy-student";
	readonly maxHp: number;
	readonly maxMp: number;
	readonly bodyShapeByte?: number;
	readonly experiencePercent?: number;
	readonly skillPoints?: number;
	readonly currentHp?: number;
	readonly currentMp?: number;
	readonly deleteReservedAt?: string;
	readonly visualLoadout: Readonly<{
		modelCodename: string;
		// Worn items (slot from the item record) and avatar items.
		items: readonly CharacterItem[];
		avatars: readonly CharacterItem[];
		animationSetName: string;
		heightScale: number;
		volumeScale: number;
	}>;
}
