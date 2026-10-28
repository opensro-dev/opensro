/*
===========================================================================

unique-banner.ts - the warning and notice banners' one current line

Each banner shows one line at a time. The latest notice replaces the
current one at once and restarts its timer (CIFNoticeWindow_LayoutAndShow
6B3180 arms 7000 ms; 6B30F0 fades over the final 2000 ms). The port-only
Experimental "Short notices" row shortens the notice banner to 5000 ms with
a 1000 ms fade.

===========================================================================
*/
import {
	SEAL_NOTICE_COLOR_MOON,
	SEAL_NOTICE_COLOR_STAR,
	type SystemNotice,
	sealDropTierFromText
} from "@/engine/foundation/gameplay/system-notices";
import { noticeText } from "@/engine/foundation/ui/notice-text";
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

// Port-only, not native: Seal of Sun drop announcements tint the top banner
// plate gold/amber instead of crimson and draw the caption in gold.
export const SUN_DROP_BANNER_COLOR = [ 180, 120, 16 ] as const;
export const SUN_DROP_BANNER_TEXT_RGB = [ 1, 215 / 255, 80 / 255 ] as const;

/*
================
SealBannerColors

A seal drop's banner colours: the plate in a deep shade of its tier,
the caption in the tier colour itself (SEAL_NOTICE_COLOR_*).
================
*/
export interface SealBannerColors {
	readonly plate: readonly [number, number, number];
	readonly textRgb: readonly [number, number, number];
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

// Port-only, not native (#1976): every seal tier colours its top-centre
// notice, not only Sun. Sun keeps the plate and caption #1640 shipped.
const SEAL_BANNER_COLORS: readonly SealBannerColors[] = [
	{ plate: [ 112, 86, 16 ], textRgb: argbTextRgb( SEAL_NOTICE_COLOR_STAR ) },
	{ plate: [ 52, 64, 94 ], textRgb: argbTextRgb( SEAL_NOTICE_COLOR_MOON ) },
	{ plate: SUN_DROP_BANNER_COLOR, textRgb: SUN_DROP_BANNER_TEXT_RGB }
];

/*
================
sealBannerColors

The banner colours for a seal tier (0 Star, 1 Moon, 2 Sun).
================
*/
export function sealBannerColors( tier: 0 | 1 | 2 ): SealBannerColors {
	return SEAL_BANNER_COLORS[tier]!;
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
				current = notice;
				started = null;
				changed = true;
			}
			if ( current && ready && started === null ) {
				started = now;
				changed = true;
			}
			const next = current && started !== null ?
				Math.floor(
					Math.max(
						0,
						Math.min( ALPHA_STEPS, (timing.showMs - (now - started)) * ALPHA_STEPS / timing.fadeMs )
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
		announces a seal drop, so the banner can colour by tier (#1976).
		================
		*/
		sealTier: () => current?.sunDrop ? 2 : (current?.text ? sealDropTierFromText( current.text ) : undefined)
	};
}

export const createUniqueBanner = createNoticeBanner;
