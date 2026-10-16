/*
===========================================================================

ui-tuning.ts - the client values a shard may tune at world entry

Port-only, not native. The world-entry bootstrap may carry a `uiTuning`
object (enterworld.BootstrapResult, values from the server's tunables
registry, class "entry"): the Experimental "Short notices" timing and the
chat's own acknowledgement deadline. Each value is taken only when it is a
whole number inside its bounds; anything else, or no object at all (an
older server, or a shard at every default), keeps today's built-in value,
so a bad or missing value can never change a session beyond its bounds.

The notice values feed only the "Short notices" row: a player who keeps
the row off sees the native 7000/2000 banner whatever the shard sends.

===========================================================================
*/

/*
================
UiTuning
================
*/
export interface UiTuning {
	// The "Short notices" banner: how long a notice shows and how much of
	// that is its fade.
	readonly shortNoticeShowMs: number;
	readonly shortNoticeFadeMs: number;
	// The port's deadline for a chat or block acknowledgement (native has none).
	readonly chatAckTimeoutMs: number;
}

// Today's built-in values and each value's accepted range (the server
// registry's bounds, game/tunables). A fade is at most 2000 and a banner at
// least 3000, so a fade never outlasts its banner.
const SHORT_NOTICE_SHOW_MS = 5000;
const SHORT_NOTICE_SHOW_MIN = 3000;
const SHORT_NOTICE_SHOW_MAX = 7000;
const SHORT_NOTICE_FADE_MS = 1000;
const SHORT_NOTICE_FADE_MIN = 500;
const SHORT_NOTICE_FADE_MAX = 2000;
export const CHAT_ACK_TIMEOUT_MS = 10000;
const CHAT_ACK_TIMEOUT_MIN = 5000;
const CHAT_ACK_TIMEOUT_MAX = 30000;

/*
================
defaultUiTuning

Today's built-in values: what a client uses until a shard says otherwise.
================
*/
export function defaultUiTuning(): UiTuning {
	return {
		shortNoticeShowMs: SHORT_NOTICE_SHOW_MS,
		shortNoticeFadeMs: SHORT_NOTICE_FADE_MS,
		chatAckTimeoutMs: CHAT_ACK_TIMEOUT_MS
	};
}

/*
================
bounded

value when it is a whole number within min..max, else fallback.
================
*/
function bounded( value: unknown, min: number, max: number, fallback: number ): number {
	return typeof value === "number" && Number.isInteger( value ) && value >= min && value <= max ? value : fallback;
}

/*
================
uiTuningFrom

The tuning a bootstrap value carries (its `uiTuning` field), value by
value.
================
*/
export function uiTuningFrom( bootstrap: unknown ): UiTuning {
	const raw = (bootstrap as { uiTuning?: unknown; } | null)?.uiTuning;
	const source = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
	return {
		shortNoticeShowMs: bounded(
			source.shortNoticeShowMs,
			SHORT_NOTICE_SHOW_MIN,
			SHORT_NOTICE_SHOW_MAX,
			SHORT_NOTICE_SHOW_MS
		),
		shortNoticeFadeMs: bounded(
			source.shortNoticeFadeMs,
			SHORT_NOTICE_FADE_MIN,
			SHORT_NOTICE_FADE_MAX,
			SHORT_NOTICE_FADE_MS
		),
		chatAckTimeoutMs: bounded(
			source.chatAckTimeoutMs,
			CHAT_ACK_TIMEOUT_MIN,
			CHAT_ACK_TIMEOUT_MAX,
			CHAT_ACK_TIMEOUT_MS
		)
	};
}
