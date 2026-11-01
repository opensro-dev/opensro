/*
===========================================================================

unique-banner.ts - the warning and notice banners' one current line

Each banner shows one line at a time. The latest notice replaces the
current one at once and restarts its timer (CIFNoticeWindow_LayoutAndShow
6B3180 arms 7000 ms; 6B30F0 fades over the final 2000 ms). The port-only
Experimental "Short notices" row shortens the notice banner to 5000 ms with
a 1000 ms fade. Port-only: a seal drop never cuts off a louder seal drop
still on screen, so a Star cannot erase a Sun.

===========================================================================
*/
import {
	SEAL_NOTICE_COLOR_MOON,
	type SystemNotice,
	sealDropTierFromText
} from "@/engine/foundation/gameplay/system-notices";
import { noticeText } from "@/engine/foundation/ui/notice-text";
import type { BannerPlateStyle } from "@/engine/foundation/ui/unique-banner";
import type { UiTuning } from "@/engine/foundation/ui/ui-tuning";

export const uniqueBannerPaths = [ "corner", "edge", "edge2" ].map( part =>
	`/assets/images/Media_extracted/interface/ifcommon/com_warning_${part}.png`
);
// 67d030 targets warning id 35, not the red notice sibling.
export const notificationBannerPaths = [ "corner", "edge", "edge2" ].map( part =>
	`/assets/images/Media_extracted/interface/ifcommon/com_notice_${part}.png`
);

/*
================
NoticeTiming

How long a banner line shows and how much of that time is its fade.
================
*/
export interface NoticeTiming {
	readonly showMs: number;
	readonly fadeMs: number;
}

// 6B3180's 0x1B58 and 6B30F0's final fade.
export const NATIVE_NOTICE_TIMING: NoticeTiming = { showMs: 7000, fadeMs: 2000 };
// Port-only, not native: the Experimental "Short notices" row (the balance
// advisor's ask, owner-approved 2026-10-15).
export const SHORT_NOTICE_TIMING: NoticeTiming = { showMs: 5000, fadeMs: 1000 };

/*
================
SealBannerStyle

A seal drop's banner weight (port-only, #1976): how loud the line is, so
the tiers read at a glance. Every tier wears the game's own banner art.
================
*/
export interface SealBannerStyle {
	readonly plate: readonly [number, number, number];
	readonly textRgb: readonly [number, number, number];
	readonly frame: BannerPlateStyle;
	readonly paths: readonly string[];
	readonly timing: NoticeTiming;
}

/*
================
argbTextRgb

A SEAL_NOTICE_COLOR_* word as a caption colour.
================
*/
function argbTextRgb( argb: number ): readonly [number, number, number] {
	return [ ((argb >>> 16) & 255) / 255, ((argb >>> 8) & 255) / 255, (argb & 255) / 255 ];
}

// The native notice's and warning's plates and caption (uniqueBannerQuads).
const NOTICE_PLATE = [ 128, 45, 67 ] as const;
const WARNING_PLATE = [ 0, 52, 92 ] as const;
const NOTICE_TEXT_RGB = [ 1, 1, 1 ] as const;
// Star is a plain native notice that leaves sooner. Moon takes the blue
// warning sibling, which its pale colour sits on. Sun stays on the red
// notice, larger and more solid, with a warm white: yellow fights the red.
const SEAL_SUN_PLATE = [ 112, 24, 40 ] as const;
const SEAL_SUN_TEXT_RGB = [ 1, 238 / 255, 200 / 255 ] as const;
const SEAL_BANNER_STYLES: readonly SealBannerStyle[] = [
	{
		plate: NOTICE_PLATE,
		textRgb: NOTICE_TEXT_RGB,
		frame: {},
		paths: notificationBannerPaths,
		timing: { showMs: 4000, fadeMs: 1000 }
	},
	{
		plate: WARNING_PLATE,
		textRgb: argbTextRgb( SEAL_NOTICE_COLOR_MOON ),
		frame: { padX: 8, padY: 2, plateAlpha: 0.7 },
		paths: uniqueBannerPaths,
		timing: { showMs: 5500, fadeMs: 1500 }
	},
	{
		plate: SEAL_SUN_PLATE,
		textRgb: SEAL_SUN_TEXT_RGB,
		frame: { padX: 24, padY: 6, plateAlpha: 0.9 },
		paths: notificationBannerPaths,
		timing: NATIVE_NOTICE_TIMING
	}
];

/*
================
sealBannerStyle

The banner weight for a seal tier (0 Star, 1 Moon, 2 Sun).
================
*/
export function sealBannerStyle( tier: 0 | 1 | 2 ): SealBannerStyle {
	return SEAL_BANNER_STYLES[tier]!;
}

/*
================
noticeSealTier

The seal tier a notice announces, if any.
================
*/
function noticeSealTier( notice: SystemNotice ) {
	if ( notice.sunDrop ) return 2;
	return notice.text ? sealDropTierFromText( notice.text ) : undefined;
}

/*
================
shownTiming

A seal line's own timing, never longer than the player's setting: the
Experimental "Short notices" row still shortens a Sun line.
================
*/
function shownTiming( notice: SystemNotice | null, timing: NoticeTiming ): NoticeTiming {
	const tier = notice ? noticeSealTier( notice ) : undefined;
	if ( tier === undefined ) return timing;
	const own = sealBannerStyle( tier ).timing;
	return { showMs: Math.min( own.showMs, timing.showMs ), fadeMs: Math.min( own.fadeMs, timing.fadeMs ) };
}

/*
================
noticeBannerRow

The notification row's plate choice (port-only, #1976): a line that
announces a seal drop wears its tier's weight. Every other notice -
academy, guild war, friends, letters, plain server notices - keeps the
native plate exactly: crimson, hugging the caption, the shipped alpha.
================
*/
export function noticeBannerRow(
	banner: Pick<ReturnType<typeof createNoticeBanner>, "sealTier">
): {
	readonly color: readonly [number, number, number];
	readonly textRgb: readonly [number, number, number];
	readonly frame: BannerPlateStyle;
	readonly paths: readonly string[];
} {
	const tier = banner.sealTier();
	if ( tier !== undefined ) {
		const style = sealBannerStyle( tier );
		return { color: style.plate, textRgb: style.textRgb, frame: style.frame, paths: style.paths };
	}
	return { color: NOTICE_PLATE, textRgb: NOTICE_TEXT_RGB, frame: {}, paths: notificationBannerPaths };
}

// The banner's alpha steps (the native quad alpha byte).
const ALPHA_STEPS = 255;

/*
================
noticeTiming

The notice banner's timing for the Experimental "Short notices" setting,
with the shard's tuning of the short timing when it sends one.
================
*/
export function noticeTiming( short: boolean, tuning?: UiTuning ): NoticeTiming {
	if ( !short ) return NATIVE_NOTICE_TIMING;
	// Port-only: the shard may tune the short timing (ui-tuning.ts); the
	// native timing above is never tuned.
	return tuning ? { showMs: tuning.shortNoticeShowMs, fadeMs: tuning.shortNoticeFadeMs } : SHORT_NOTICE_TIMING;
}

/*
================
createNoticeBanner
================
*/
export function createNoticeBanner( kind: "banner" | "notificationBanner" = "banner" ) {
	let sequence = 0, current: SystemNotice | null = null, started: number | null = null, alpha = 0;
	return {
		/*
		================
		reset
		================
		*/
		reset() {
			sequence = 0;
			current = null;
			started = null;
			alpha = 0;
		},
		/*
		================
		step

		Takes the newest notice of this banner's kind and advances its fade;
		true when what the banner shows changed.
		================
		*/
		step(
			notices: readonly SystemNotice[],
			now: number,
			ready: boolean,
			copy?: ( key: string ) => string,
			timing: NoticeTiming = NATIVE_NOTICE_TIMING
		) {
			let changed = false;
			for ( const notice of notices ) {
				if ( !notice[kind] || notice.sequence === undefined || notice.sequence <= sequence ) continue;
				// 67D034 returns on empty text without replacing the active warning.
				// Wait for localization admission before deciding whether text is empty.
				if ( copy && !ready ) continue;
				sequence = notice.sequence;
				if ( copy && !noticeText( copy, notice, "banner" ) ) continue;
				// A line adopted in this batch has not started yet, and still counts.
				const louder = current && (alpha > 0 || started === null) ? noticeSealTier( current ) : undefined,
					incoming = noticeSealTier( notice );
				if ( louder !== undefined && incoming !== undefined && incoming < louder ) continue;
				current = notice;
				started = null;
				changed = true;
			}
			if ( current && ready && started === null ) {
				started = now;
				changed = true;
			}
			const shown = shownTiming( current, timing );
			const next = current && started !== null ?
				Math.floor(
					Math.max(
						0,
						Math.min( ALPHA_STEPS, (shown.showMs - (now - started)) * ALPHA_STEPS / shown.fadeMs )
					)
				) / ALPHA_STEPS :
				0;
			if ( next !== alpha ) {
				alpha = next;
				changed = true;
			}
			return changed;
		},
		/*
		================
		value
		================
		*/
		value: ( copy: ( key: string ) => string ) => current ? noticeText( copy, current, "banner" ) : "",
		/*
		================
		alpha
		================
		*/
		alpha: () => alpha,
		/*
		================
		sunDrop
		================
		*/
		sunDrop: () => Boolean( current?.sunDrop ),
		/*
		================
		sealTier

		The current line's seal tier (0 Star, 1 Moon, 2 Sun) when it
		announces a seal drop, so the banner can set its weight (#1976).
		================
		*/
		sealTier: () => current ? noticeSealTier( current ) : undefined
	};
}

export const createUniqueBanner = createNoticeBanner;
