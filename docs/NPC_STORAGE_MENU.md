# NPC window visibility when opening storage

The v1.150 storage menu action hides `CIFNpcWindow` before sending its
request. It does not end the conversation, release the target, or wait for
storage to open. This is window visibility independent of the primary panel.

The supplied `SRO_Client.exe` has SHA-256
`f6580bf65213c0c074efe312514c55c73040667e7a9ad1aae9b8595d48ccd3a2`,
image base `0x400000`. The following claims were checked by reading the
instructions and the action dispatch tables in that executable. No claim
here depends on a reverse-engineering function name or a server binary.

| Claim | Instruction evidence | Status |
| --- | --- | --- |
| The personal storage row is action 3, variant 0. | `0x5D94C4–0x5D94D3` checks capability 4 and creates action 3; `0x5D6B2B` initializes variant `+0x10` to zero. The tables at `0x5DBB9C` and `0x5DBAFC` route action 3 to `0x5DA489`, whose zero-variant branch goes to `0x5DA7D6`. | instruction-checked |
| Personal storage hides the NPC parent before either request path. | `0x5DA7D6–0x5DA7E5` retrieves the parent through `0x9FCD60` (`[this+0x3C]`) and calls visibility vmethod `+0x58` with zero. Only afterward does `0x5DA7F4` check the loaded flag. The loaded path requests `0x7338` with function 4; the unloaded path at `0x5DA8D0` requests `0x72C3`. | instruction-checked |
| Guild storage also hides the parent before requesting its function. | Action `0x1D` routes to `0x5DAFA9`; `0x5DAFA9–0x5DAFB8` hides the parent. `0x5DAFBB` starts request `0x7338`, with function `0x4000` at `0x5DB021`. | instruction-checked |
| Hiding the NPC window does not release the interaction. | The storage action paths above continue into their service requests. Vtable `0xBE3A34` has `0x5DBE00` at slot `+0x58` (`0xBE3A8C`); its hide path performs UI cleanup and delegates to base visibility (`0x53D420` at `0x5DBEA1`), not an NPC release. | instruction-checked |
| A fresh NPC selection prepares its menu independently of storage-panel selection. | The selection handler at `0x7650C5–0x7650DC` prepares the NPC window through `0x5DBCE0(1)` and sets the selected entity through `0x5DC290`. `0x5DBCE0` hides then shows its talk child for mode 1. | instruction-checked |
| Clicking the same NPC selects it again only while its window is hidden. | `0x692BC8` fetches the NPC window and reads its visible byte `+0x6D` (`0x9FCDA0`), which the base visibility setter `0x53D420` writes through `0x9FCD70`. Only when the window is visible and its stored NPC (`+0x7C4`, `0x5DBD70`) matches the click does it skip; otherwise it requests `0x745A` at `0x692C18`. | instruction-checked |

The port stores a versioned NPC-window visibility receipt in the conversation
owner. The HUD hides immediately on the click; the worker also hides before
issuing either storage request. Receipts distinguish a fresh selection of the
same NPC from an old snapshot, including when close and reselection occur
between two HUD frames. Changing Inventory, Character or another primary
panel has no authority to show the NPC window. A storage command the worker
refuses (its target changed after the HUD snapshot the player clicked on)
publishes a newer receipt marked shown before it throws, so the refusal
itself lifts the hide its click caused; the conversation stays open.

`npc-storage-window.test.mjs` exercises request ordering, personal storage's
cached and uncached paths, guild storage, preserved target and interaction
lock, delayed worker receipts, panel changes, scroll rejection while hidden,
reselection of the same NPC without an intermediate closed snapshot,
clicking the same NPC again after storage hid its window, and storage
commands refused after the NPC left view.
These tests execute the shipped port; the native evidence is static analysis,
not a full client emulation.
