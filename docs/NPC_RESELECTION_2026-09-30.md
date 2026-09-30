# BUG-042: reopening the same NPC

Repeated selection could close a granted conversation while leaving its target
selected. The target owner then suppressed every subsequent request for that
same identity. Selecting another object happened to escape the stale state.
This affected the common NPC boundary used by dialogue and services.

## Native admission rule

The v1.150 client disassembly at `692BA0` was checked directly:

- `692BC8` calls the talk-window lookup (`69AF10`, window ID `1E`).
- `692BD1` calls `9FCDA0`, which reads the visibility byte at window `+6D`.
- `692BD8` branches to the send path when the window is hidden.
- For a visible window, `5DBD70` reads its bound NPC at `+7C4`;
  `692BE8..692BEC` compares that identity with the requested NPC.
- A visible conversation for the same NPC returns without sending. Otherwise
  `692C18..692C29` constructs `745A` and appends the requested four-byte GID.

The port therefore needs both target identity and conversation lifetime to
decide whether a request is redundant. A retained target alone is insufficient.

Two unnamed functions encountered during the investigation were also identified
from their instructions: `5D03E0` is `UI_HideMenuAndRequestRestartType2`, and
`700020` is `CPSMission_SendLogoutRestartRequest70B7`. Both labels and explanatory
comments were saved to the client database; symbol read-back from snapshot 209
confirmed persistence. These restart functions are incidental findings, not
evidence for the NPC selection rule.

## Shared owners and related paths

| Boundary | Owner and behavior |
| --- | --- |
| Direct NPC and gate selection | `gameplay.ts` clears the previous conversation only after a new request was sent. Coalesced clicks preserve menu, waiting/uncertain/ready dialogue, and interaction lock. |
| Closed service conversation | `targeting.ts` permits a fresh grant for the retained NPC or gate when the conversation owner reports a closed pane. |
| Gate approach arrival | The second `targeting.select` caller supplies the same reopening fact after movement reaches a gate. Ordinary and fortress gates share this rule. |
| Pending select | Repeated clicks still coalesce and do not extend the request deadline. |
| Close button, Talk End and Escape | Existing close/release commands retain the untagged `B4B3` acknowledgement barrier. Reopening cannot bypass it. |
| Server select | `HandleObjectSelect` already grants every valid request, including the same NPC, and clears its previous dialogue. A server regression now pins this behavior before and after release. |
| Server release | `HandleTargetRelease` validates the selected identity, clears selection/dialogue, and acknowledges closure. No server behavior change is required. |

Ripgrep mapping covered both production selection callers, all `createTargeting`
tests, NPC dialogue/service state, direct and arrival gate paths, and the server
selection/release handlers. The protocol and asset schema do not change.

## Reproduction and regression coverage

`tests/runtime/npc-selection-lifecycle.test.mjs` exercises eight dispatcher cases:
zero-capability NPCs, gate arrival, interaction-lock retention, repeated NPC/gate selection, all active
dialogue phases, service-window reopening, release barriers, and failed/successful
target switches. `npc_reselection_test.go` pins the server half of the contract.

`tests/browser/npc-reopen-live.test.mjs` uses an authenticated scratch session,
normal dock entry, the runtime gameplay command boundary, real NPC replies and
the DOM close/Escape controls. It requires a dedicated single-character roster.
`SRO_PROBE_CHARACTER` names that actor; `SRO_NPC_REOPEN_POSITION` optionally gives
a normal walking destination when the initial spawn has no nearby NPC. The test
does not rewrite served source or responses. It records only NPC socket frames
and starts its Playwright trace after authentication and world admission.

The pre-fix browser reproduced the failure on Soldier Fengil, GID `200008`:
first selection opened the menu; repeated selection changed it to `closed`.
The fixed client preserves that menu and reopens the same NPC after both close
controls. The release record and public testing status are maintained separately
from this implementation evidence.
