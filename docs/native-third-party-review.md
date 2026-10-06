# Third-party native review — captured revisions

The reviewed scope is the complete diff of each revision below, including tests, build scripts, manifests and deleted lines. The audit read 94 per-PR file changes (4,035 additions and 236 deletions). A line review is not a proof that every reachable native instruction has a corresponding port.

## Decisions

| PR | Reviewed head | Disposition |
| --- | --- | --- |
| #184 | `149f56594c16b5780c93dd45dd8904deff46d067` | Native portrait selection corrected in #197. |
| #185 | `0a192e07a157204efb6fa6adbc8cfb41369203f2` | Non-native FPS/build diagnostics; owner approval pending. Production builder bypasses the proposed Vite defines. |
| #186 | `acf5d35b1f23272b21a3ce90007187a8fa5067a1` | Non-native welcome and first-login tour; owner approval pending. Proposed assets also fail the asset declaration guard. |
| #187 | `a132c9f0249308d210d2e6966cf019aef8788723` | Non-native chat date hover; owner approval pending. |
| #191 | `c13d9d8025b9aabe1e2763639d6a77b145c880c7` | European skill native corrections in #197. Native Mana Wind and Temptation rules explicitly approved. |
| #192 | `ac50e13ada6e6b7f3fcf63e8d0043183b557e18d` | Browser password-manager integration; separate owner approval pending. |
| #194 | `9963889163b08356cfdd74acb70b938ced4a6cf4` | Browser replay recovery URL change; separate owner approval pending. |
| #195 | `62466dc8922ae1894c8f6e2e67dd788845c05e1b` | Graphics modernization; each visual deviation requires separate owner approval. Includes grading of UI and configuration-path concerns. |

Own-author PRs #189, #196, #197 and #198 are outside this third-party census. No third-party PR was merged, no deployment was performed, and no author was messaged. The shared checkout was left untouched.

## Native correction evidence

| Behavior | Original evidence | Port/check |
| --- | --- | --- |
| Local portrait selects self without network selection; pending response ownership survives | Client 6B3E90, 6813E0 | #197 portrait runtime and UI tests |
| Every linked attack stage pays and revalidates its own resources | Server 4AE590, 586700, 593540 | #197 linked-stage and HP arithmetic tests |
| Resistance picks unsigned maximum grade, then unsigned maximum flat | Server 5999E0 | #197 2,401 original-x86 cases and reproducible generator |
| Temptation uses base grade and 0x3033 refusal without extra quest/event exclusions | Server 58CC70 | #197 authored rows and runtime refusals |
| Chain healing includes the authored caster, orders by native HP ratio, and compounds Mana Wind 100→50→25 | Server 58C170, 594397, 5A0850 | #197 selection and healing tests |
| Fortress indicators and countdown timers | Client 6B5370, 6B5A50, 7E22F0, A00BE0 | #197 HUD/state/rendering tests |

## Newly discovered native work

The dependent fortress battle branch owns score accounting, party credit, rank skills, battle records, battle notices, siege death classification, and war-end cleanup. Its evidence and boundaries are in [fortress-battle-native.md](fortress-battle-native.md). It reuses the existing fortress, character, effect, party, union and durable-store owners.

The next separate fortress audit is staff hiring and holder flags: server 620090 / 4E05A0 and 6321CA expose a different stream from battle scores. This branch does not claim that staff-hire mutations or holder-flag synchronization are complete. The existing guild-war stub in `pkrelation.go` is also outside the battle-record owner and remains a full-game port item.

The strongest-usable HP/MP replacement remains the previously approved exception. Native Mana Wind and Temptation behavior supersede the old comments. No other non-native proposal is implicitly approved.

## File coverage

### PR #184

- `apps/client-next/src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts` (+9, −1)
- `apps/client-next/src/engine/runtime/ui/ui.ts` (+26, −2)
- `apps/client-next/tests/runtime/gameplay.test.mjs` (+15, −0)
- `apps/client-next/tests/runtime/ui-behavior.test.mjs` (+23, −0)

### PR #185

- `apps/client-next/execution-contract.json` (+1, −1)
- `apps/client-next/src/engine/contracts/runtime.ts` (+2, −0)
- `apps/client-next/src/engine/ownership.json` (+2, −1)
- `apps/client-next/src/engine/runtime/build-info/build-info.ts` (+178, −0)
- `apps/client-next/src/engine/runtime/platform/platform.ts` (+3, −1)
- `apps/client-next/src/engine/runtime/runtime.ts` (+6, −1)
- `apps/client-next/tests/runtime/build-info.test.mjs` (+127, −0)
- `apps/client-next/tools/verify-capabilities.mjs` (+8, −1)
- `apps/client-next/vite.config.mjs` (+81, −27)
- `apps/server/cmd/operations/sro-nomad/deployment.go` (+23, −1)
- `apps/server/internal/agent/server/build.go` (+95, −0)
- `apps/server/internal/agent/server/build_test.go` (+80, −0)
- `apps/server/internal/agent/server/releaseprotocol_test.go` (+1, −0)
- `apps/server/internal/agent/server/server.go` (+4, −0)
- `scripts/checks/format-baseline.txt` (+0, −1)

### PR #186

- `apps/client-next/execution-contract.json` (+8, −1)
- `apps/client-next/index.html` (+1, −1)
- `apps/client-next/src/engine/foundation/gameplay/quickslots.ts` (+1, −1)
- `apps/client-next/src/engine/foundation/ui/onboarding-steps.ts` (+166, −0)
- `apps/client-next/src/engine/foundation/ui/skill-press-feedback.ts` (+16, −4)
- `apps/client-next/src/engine/ownership.json` (+2, −1)
- `apps/client-next/src/engine/runtime/bug-report/dialog.ts` (+2, −1)
- `apps/client-next/src/engine/runtime/onboarding/onboarding.ts` (+428, −0)
- `apps/client-next/src/engine/runtime/platform/loading.css` (+240, −0)
- `apps/client-next/src/engine/runtime/platform/platform.ts` (+37, −1)
- `apps/client-next/src/engine/runtime/runtime.ts` (+3, −0)
- `apps/client-next/tests/runtime/onboarding-steps.test.mjs` (+60, −0)
- `apps/client-next/tools/verify-capabilities.mjs` (+11, −3)
- `apps/server/cmd/operations/sro-nomad/deployment.go` (+6, −1)
- `apps/server/cmd/operations/sro-nomad/onboarding_test.go` (+24, −0)
- `apps/server/cmd/services/sro-agent/main.go` (+2, −0)
- `apps/server/internal/agent/onboarding/onboarding.go` (+32, −0)
- `apps/server/internal/agent/onboarding/onboarding_test.go` (+26, −0)
- `apps/server/internal/agent/server/onboarding.go` (+33, −0)
- `apps/server/internal/agent/server/onboarding_test.go` (+43, −0)
- `apps/server/internal/agent/server/releaseprotocol_test.go` (+1, −0)
- `apps/server/internal/agent/server/server.go` (+5, −0)
- `apps/server/ops/nomad/jobs/agent.nomad.hcl` (+7, −0)

### PR #187

- `apps/client-next/src/engine/foundation/ui/chat-time.ts` (+19, −2)
- `apps/client-next/tests/runtime/chat-time.test.mjs` (+19, −0)

### PR #191

- `apps/server/internal/game/action/activeeffect.go` (+4, −0)
- `apps/server/internal/game/action/offensiveintent.go` (+6, −0)
- `apps/server/internal/game/action/playerroll.go` (+28, −0)
- `apps/server/internal/game/action/skillcombat.go` (+3, −0)
- `apps/server/internal/game/action/skillhealingdivision_test.go` (+84, −0)
- `apps/server/internal/game/action/skillhpchain_test.go` (+84, −0)
- `apps/server/internal/game/action/skilloverheal_test.go` (+156, −0)
- `apps/server/internal/game/action/skillparty.go` (+16, −4)
- `apps/server/internal/game/action/skillrecovery.go` (+26, −3)
- `apps/server/internal/game/action/skillresistancebuff_test.go` (+104, −0)
- `apps/server/internal/game/action/targetinteract.go` (+1, −1)
- `apps/server/internal/game/action/temptation.go` (+25, −1)
- `apps/server/internal/game/action/temptation_test.go` (+16, −2)
- `apps/server/internal/game/combat/passives.go` (+4, −14)
- `apps/server/internal/game/combat/statusresistance.go` (+64, −0)
- `apps/server/internal/game/enterworld/skillfixeddamage.go` (+45, −15)
- `apps/server/internal/game/enterworld/skillfixeddamage_test.go` (+85, −15)
- `apps/server/internal/game/enterworld/skillhealingdivision_test.go` (+45, −0)
- `apps/server/internal/game/enterworld/skilloffense.go` (+4, −3)
- `apps/server/internal/game/enterworld/skillrecovery.go` (+34, −0)
- `apps/server/internal/game/enterworld/skillrecovery_test.go` (+7, −5)
- `apps/server/internal/game/enterworld/skillresistancebuff_test.go` (+51, −0)
- `apps/server/internal/game/enterworld/skillsequence.go` (+6, −1)
- `apps/server/internal/game/enterworld/skillsequence_test.go` (+43, −3)
- `apps/server/internal/game/enterworld/skilltimedeffect.go` (+23, −3)

### PR #192

- `apps/client-next/index.html` (+1, −1)
- `apps/client-next/src/engine/runtime/platform/ui/ui.ts` (+6, −0)

### PR #194

- `apps/client-next/src/engine/foundation/media/replay-window.ts` (+14, −0)
- `apps/client-next/src/engine/runtime/bug-report/bug-report.ts` (+3, −2)
- `apps/client-next/tests/runtime/bug-report-media.test.mjs` (+13, −1)

### PR #195

- `apps/client-next/src/bootstrap.ts` (+59, −7)
- `apps/client-next/src/engine/contracts/runtime.ts` (+1, −0)
- `apps/client-next/src/engine/contracts/scene.ts` (+4, −0)
- `apps/client-next/src/engine/foundation/rendering/geometry.ts` (+1, −0)
- `apps/client-next/src/engine/foundation/rendering/world-environment.ts` (+209, −58)
- `apps/client-next/src/engine/ownership.json` (+1, −0)
- `apps/client-next/src/engine/runtime/assets/worker/loader.ts` (+23, −0)
- `apps/client-next/src/engine/runtime/renderer/device/device.ts` (+17, −4)
- `apps/client-next/src/engine/runtime/renderer/device/finish.ts` (+261, −0)
- `apps/client-next/src/engine/runtime/renderer/device/geometry.ts` (+1, −1)
- `apps/client-next/src/engine/runtime/renderer/device/pipelines.ts` (+70, −7)
- `apps/client-next/src/engine/runtime/renderer/renderer.ts` (+7, −3)
- `apps/client-next/tests/browser/finish.test.mjs` (+130, −0)
- `apps/client-next/tests/runtime/gpu-retirement.test.mjs` (+8, −1)
- `apps/client-next/tests/runtime/lifecycle.test.mjs` (+7, −2)
- `apps/client-next/tests/runtime/world-stream.test.mjs` (+27, −5)
- `apps/client-next/tools/verify-capabilities.mjs` (+18, −0)
- `docs/GRAPHICS_UPGRADE_2026-10-05.md` (+163, −0)
- `scripts/build/world/objects/buildTitleSectorObjectResources.mjs` (+151, −19)
- `scripts/checks/format-baseline.txt` (+0, −2)
