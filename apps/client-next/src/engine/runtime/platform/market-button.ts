/*
===========================================================================

market-button.ts - the consignment market's button beside FPS

Port-only, not native (SRO_CONSIGNMENT): v1.150 has no consignment. The
page shell's tool row (index.html #fps-chip) carries one DOM button that
opens the market window from anywhere in the world; platform.ts shows it
from the UI's semantics and the click goes to ui.ts as "market-toggle".

===========================================================================
*/

/*
================
createMarketButton

Port-only, not native (SRO_CONSIGNMENT): the consignment market's button
in the FPS chip, gilded so it reads apart from the tools beside it. It
shows only in the world on a shard that runs the market (UiSemantics
marketButton); platform.ts owns its click, which toggles the window.
================
*/
export function createMarketButton( chip: HTMLElement | null ): HTMLButtonElement {
	const button = document.createElement( "button" );
	button.id = "market-toggle";
	button.className = "sro-fps-chip__toggle sro-market-toggle";
	button.type = "button";
	button.title = "Consignment market";
	button.setAttribute( "aria-label", "Consignment market" );
	button.ariaPressed = "false";
	button.hidden = true;
	// A market stall: a striped awning over a counter.
	button.innerHTML = '<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">' +
		'<path d="M2 6 3 2h10l1 4z" fill="currentColor"/>' +
		'<path d="M3 7h10v7H3z" fill="none" stroke="currentColor" stroke-width="1.5"/>' +
		'<path d="M6 14v-4h4v4" fill="currentColor"/></svg>';
	if ( chip ) chip.insertBefore( button, chip.firstChild );
	else document.body.append( button );
	return button;
}
