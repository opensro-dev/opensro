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
/** The Agent's answer to one filed bug report (worker/session/http), by operation. */
export interface BugReportResult {
	readonly operationId: number;
	readonly ok: boolean;
	/** The Agent's refusal code (UNAUTHORIZED, RATE_LIMITED, ...), when it refused. */
	readonly code?: string;
	readonly retryAfter?: number;
}
export type SessionCommand =
	| WorldExitCommand
	| { readonly kind: "chat-blocks"; readonly value: readonly string[]; }
	// The lines the browser kept for the entering character (chat-history.ts;
	// port-only, not native - default by owner decision 2026-11-26), offered
	// ahead of enter-world; the worker never touches storage itself.
	| {
		readonly kind: "chat-history";
		readonly name: string;
		readonly lines: readonly import("@/engine/foundation/gameplay/chat-history").StoredChatLine[];
	}
	| {
		readonly kind: "experimental-gameplay";
		readonly value: import("@/engine/foundation/ui/experimental-options").ExperimentalGameplay;
	}
	| {
		readonly kind: "game-options";
		readonly value: import("@/engine/foundation/gameplay/game-options").GameOptions;
	}
	| CharacterOperationCommand
	| {
		readonly kind: "gameplay";
		readonly command: import("./gameplay").GameplayCommand;
		// The main thread's input sequence, stamped by the simulation host
		// (movement-input.ts, #1441a).
		readonly inputSeq?: number;
	}
	| { readonly kind: "enter-world"; readonly character: string; }
	| { readonly kind: "disconnect"; }
	| { readonly kind: "restart"; }
	| { readonly kind: "reconnect"; }
	| { readonly kind: "world-ready"; readonly travelRevision?: number; }
	// A main-thread fault the session reports to the Agent: a non-fatal
	// degradation (an asset cancellation never acknowledged), a refused
	// world entry the player sees as "(S2)", or a terminal asset failure
	// a stream gave up retrying (#2337).
	| {
		readonly kind: "client-report";
		readonly incidentKind: "degraded" | "runtime" | "asset";
		readonly code: string;
		readonly message: string;
		// How long the page was hidden during the entry a report names
		// (entry-visibility.ts), for the world_entry_rejected incident.
		readonly tabHiddenMs?: number;
		// A thrown error's stack, when the reporter caught one (#2559):
		// the message is capped far below a useful stack, so the stack
		// rides its own field (ClientIncident.stack) and is bounded by
		// the session when it files.
		readonly stack?: string;
	}
	| LoginCommand
	// A composed bug report to file with the session's own credential
	// (#4109): the parts ride to the session owner, which posts them as
	// itself, and the answer lands in bugReportResult below.
	| {
		readonly kind: "bug-report";
		readonly operationId: number;
		readonly description: string;
		readonly meta: string;
		readonly diagnostics?: Blob;
		readonly mediaField?: "clip" | "screenshot";
		readonly media?: Blob;
	}
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
	pingMs?: number | null;
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
	/** The last filed bug report's answer, matched by operationId. */
	bugReportResult?: BugReportResult;
	character?: string;
	restoringWorld?: boolean;
	entityCount?: number;
	crestPrefix?: number;
	nativeServerName?: string;
	// The shard's port-only suit-look rules as the roster published them
	// (SRO_JOB_SUIT_HIDES_ARMOR / SRO_MOONLIT_THIEF_SUIT); absent is native.
	jobSuitHidesArmor?: boolean;
	moonlitThiefSuit?: boolean;
	// A server answered that it speaks another release protocol (426): this
	// page must be refreshed into the newer release.
	releaseOutdated?: boolean;
	marksBase?: string;
	// Static atlas identity, never creature/session data. Loaded by the optional HUD.
	huntingGuide?: import("./hunting-guide").HuntingGuideSource;
	divisionId?: string;
	error?: string;
	// The last world failure's evidence, for the
	// world_entry_rejected incident (#2311 item 4): the world phase
	// that failed (a mid-world drop reports "world"), how much wire
	// arrived on the attempt that failed, the typed refusal the
	// mint answered with when it answered one (#2557: "MAINTENANCE"
	// files apart from real rejections), and closeReason, the failure's
	// code (server_bye_3 when the server's BYE named the end), absent
	// when the failure named none (#2558).
	entryFailure?: {
		readonly stage: string;
		readonly bytesReceived: number;
		readonly framesReceived: number;
		readonly refusal?: string;
		readonly closeReason?: string;
		// The native error code a refused EnterWorld result carried; the
		// character screen shows its message (characterStatus).
		readonly nativeErrorCode?: number;
	};
	// The transport session the world is on (NetworkOwner
	// .transportSessionId): the world_entry_rejected incident joins
	// itself to the transport's session log lines by it (#2558).
	transportSessionId?: string;
	code?: string;
	nativeTitleStatus?: number;
	nativeTitleArgument?: number;
	// The blocked login's two appended strings (#2271): the Agent sends
	// them only with the status 3 argument 1 refusal, after the password
	// verified.
	nativeTitleBlockReason?: string;
	nativeTitleBlockEnd?: string;
	servers?: readonly ServerRecord[];
	characters?: readonly CharacterRecord[];
}>;
export interface SessionOwner {
	isWorldReady(): boolean;
	command( command: SessionCommand, now?: number ): void;
	step( now?: number ): SessionState | null;
	takeWorld(): import("./world").WorldBatch | null;
	// The transport's frame journal since the last take (NetworkOwner.takeJournal).
	takeNetJournal(): Float64Array | null;
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
	// Port-only, not native (#3925): an announced shard the operator has
	// not opened (server SRO_COMING_SOON_SHARDS). Its transportUrl is
	// empty: it can be shown but never entered. Absent is false.
	readonly comingSoon: boolean;
	readonly transportUrl: string;
}
// One worn or avatar item of a character-list row: native (RefItemID, plus).
export interface CharacterItem {
	readonly refObjId: number;
	readonly plus: number;
}
// The shard's port-only suit-look rules (SRO_JOB_SUIT_HIDES_ARMOR,
// SRO_MOONLIT_THIEF_SUIT); both false is native. The roster and the world
// entry publish the same pair, and a character is dressed by them the
// same way on the select dock as in the world (#4128).
export interface SuitRuleFlags {
	readonly jobSuitHidesArmor: boolean;
	readonly moonlitThiefSuit: boolean;
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
	// Port-only (#1689): a queued name change refused at this login, and
	// why. Absent when no rename was queued or the rename applied.
	readonly renameCancelled?: import("@/engine/foundation/gameplay/name-change").RenameCancelledReason;
	readonly maxHp: number;
	readonly maxMp: number;
	readonly bodyShapeByte?: number;
	readonly experiencePercent?: number;
	readonly skillPoints?: number;
	readonly currentHp?: number;
	readonly currentMp?: number;
	readonly deleteReservedAt?: string;
	// The instant the server archives the character (its deletion window,
	// native seven days or the shard's SRO_QUICK_CHARACTER_DELETE day).
	readonly deleteMaturesAt?: string;
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
