/*
===========================================================================

experimental-hud.ts - saved and draft experimental preferences

Opening starts a fresh draft. Only Confirm changes effective preferences;
closing or Escape leaves the saved value intact.

===========================================================================
*/

import { experimentalOptions, type ExperimentalOptions } from "@/engine/foundation/ui/experimental-options";

/*
================
createExperimentalHud
================
*/
export function createExperimentalHud() {
	let saved = experimentalOptions(), draft = saved;
	return {
		/*
		================
		restore
		================
		*/
		restore( value: ExperimentalOptions ) {
			saved = experimentalOptions( value );
			draft = saved;
		},
		/*
		================
		open
		================
		*/
		open() {
			draft = saved;
		},
		/*
		================
		toggleChatTimestamps
		================
		*/
		toggleChatTimestamps() {
			draft = { ...draft, chatTimestamps: !draft.chatTimestamps };
		},
		/*
		================
		toggleDeveloperDiagnostics
		================
		*/
		toggleDeveloperDiagnostics() {
			draft = { ...draft, developerDiagnostics: !draft.developerDiagnostics };
		},
		/*
		================
		reset
		================
		*/
		reset() {
			draft = experimentalOptions();
		},
		/*
		================
		confirm
		================
		*/
		confirm() {
			saved = draft;
			return saved;
		},
		/*
		================
		state
		================
		*/
		state() {
			return { saved, draft };
		}
	};
}
