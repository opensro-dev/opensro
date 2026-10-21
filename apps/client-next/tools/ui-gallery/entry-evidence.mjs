/*
===========================================================================
ui-entry-evidence.mjs - target-specific witnesses for synthetic UI mounts

These are production semantic identities, not new window implementations.
Every required control must be present. A companion or prerequisite cannot
stand in for the requested surface. This does not prove visible menu entry.
===========================================================================
*/

/** @type {Record<string, readonly string[]>} */
export const ENTRY_WITNESSES = {
	"Academy": [ "academy-invite", "select-window:Academy" ],
	"Academy honor": [ "academy-honor-close", "academy-honor-confirm" ],
	"Academy Matching": [ "academy-search", "academy-refresh" ],
	"Actions": [ "action:1000", "select-window:Actions" ],
	"Alchemy": [ "alchemy-mode:compound", "alchemy-start" ],
	"Auto Potion": [ "potion-enable:hp", "potion-percent:hp" ],
	"Blocking": [ "blocking-list", "blocking-add" ],
	"COS inventory": [ "window-drag:COS inventory", "cos-tab:0" ],
	"Character": [ "stat-str", "stat-int" ],
	"Chat": [ "chat-channel:1", "chat-send" ],
	"Experimental": [ "experimental-tab:0", "experimental-render-scale:100" ],
	"Fortress war application": [ "fortress-war-close", "fortress-war-slot:1" ],
	"Fortress war schedule": [ "fortress-schedule-close", "fortress-schedule-prev" ],
	"Fortress tax": [ "fortress-tax-close", "fortress-tax-help:19" ],
	"Fortress production": [ "fortress-production-close", "window-drag:Fortress production" ],
	"Game Guide": [ "guide-sidebar", "guide-up" ],
	"Guild": [ "guild-tab:0", "guild-tab:1" ],
	// Guild tools is an intentional registry alias of the Guild owner.
	"Guild tools": [ "guild-tab:0", "guild-tab:1" ],
	"Inventory": [ "inventory-page:0", "inventory-gold" ],
	"Job ranking": [ "job-rank-close", "job-rank-prev" ],
	"Magic Pang": [ "gacha-prize:1", "gacha-prize:2" ],
	"Magic option": [ "magic-option-close", "magic-option-slot" ],
	"Map": [ "map-pan", "map-follow" ],
	"Option": [ "option-tab:0", "option-ok" ],
	"Party": [ "party-invite", "select-window:Party" ],
	"Party Matching": [ "party-match:purpose", "party-search-name" ],
	"Quests": [ "quest-scroll-up", "quest-scroll-thumb" ],
	"Shop": [ "window-drag:Shop", "shop-repair:GDR_STORE_BTN_REPAIRALL" ],
	"Skills": [ "skill-scroll-up", "skill-scroll-thumb" ],
	"Skin change": [ "skin-close", "skin:male" ],
	"Stall network": [ "stall-net-close", "stall-net-search" ],
	"Storage": [ "storage-slot:0", "storage-gold" ],
	"System": [ "system-restart", "system-exit" ],
	"Global chat": [ "wholechat-close", "wholechat-text" ],
	"Exchange": [ "exchange-close", "exchange-my:0" ],
	"Stall": [ "stall-close", "stall-slot:0" ]
};

/*
================
entryEvidence

Unknown targets fail closed. Presence proves only this mounted presentation;
disabled controls are valid witnesses, not proof that their action is allowed.
================
*/
/** @param {string} name @param {readonly {id: string}[]} controls */
export function entryEvidence( name, controls ) {
	const required = ENTRY_WITNESSES[name] ?? [];
	const ids = new Set( controls.map( control => control.id ) );
	const missing = required.filter( id => !ids.has( id ) );
	return {
		required,
		missing,
		admitted: required.length > 0 && missing.length === 0,
		entryFlow: "unverified; synthetic presentation admission"
	};
}
