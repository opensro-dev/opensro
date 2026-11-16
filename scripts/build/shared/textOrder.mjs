/*
===========================================================================

textOrder.mjs - the one text order every build output sorts by

Packs, indexes, manifests and the server game-data bundle list their
entries in sorted order, and the order is part of their bytes: a
different order is a different pack hash, a different URL, and every
player downloads it again. The build sorted with String#localeCompare,
whose order is the host's default locale: the same tree built on a host
set to another locale (tr-TR orders "i" and "I" differently, and some
locales sort "_" and "-" apart) would pack different bytes.

compareText pins that order to en-US, the locale every release host
has built with so far, so today's bytes stay exactly as they are and a
build on any host gives the same ones. It is a collator, not codepoint
order: "a_b" sorts before "a-b" and "a" before "A", as they always have.
One collator is built once; localeCompare with a locale argument would
build one per call.

===========================================================================
*/

// The locale the published bytes were built in (Intl.Collator default on
// the release host, 2026-11-16). Changing it reorders packs: a full
// re-download for every player.
export const TEXT_ORDER_LOCALE = "en-US";

const collator = new Intl.Collator( TEXT_ORDER_LOCALE );

/*
================
compareText

Array#sort comparator over strings in the build's pinned text order.
================
*/
export function compareText( left, right ) {
	return collator.compare( left, right );
}
