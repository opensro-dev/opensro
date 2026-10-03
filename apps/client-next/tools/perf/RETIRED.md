# Retired profiling tools

Profiling tooling now lives in `tools/perf` (core, bench, analyze). These
files were removed when it was consolidated. They remain in git history:
restore any of them with

```
git checkout 84324f5 -- <path>
```

| Path | Why it went |
| --- | --- |
| `apps/client-next/tools/profile-world.mjs` | the old world profiler; tools/perf/bench/fps-bench.mjs measures the same scenarios with the shared perf core |
| `apps/client-next/tools/summarize-cpu.mjs` | CPU profile summary; tools/perf/analyze/profile.mjs reads CPU and heap profiles |
| `apps/client-next/tests/runtime/cpu-summary.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/animation-ceiling.mjs` | probe used only by profile-world |
| `apps/client-next/tests/runtime/animation-ceiling.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/animation-phase-profiler.mjs` | probe used only by profile-world |
| `apps/client-next/tests/runtime/animation-phase-profiler.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/character-facing-probe.mjs` | probe used only by profile-world |
| `apps/client-next/tools/lib/combat-probe.mjs` | probe used only by profile-world |
| `apps/client-next/tools/lib/frame-analysis.mjs` | probe used only by profile-world |
| `apps/client-next/tests/runtime/frame-analysis.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/frame-profiler.mjs` | profile-world frame profiler; the bench installs its own frame probe |
| `apps/client-next/tests/runtime/frame-profiler.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/frame-window.mjs` | probe used only by profile-world |
| `apps/client-next/tests/runtime/frame-window.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/guide-ui-probe.mjs` | probe used only by profile-world |
| `apps/client-next/tools/lib/inventory-probe.mjs` | probe used only by profile-world |
| `apps/client-next/tools/lib/movement-trace.mjs` | probe used only by profile-world |
| `apps/client-next/tests/runtime/movement-trace.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/party-probe.mjs` | probe used only by profile-world |
| `apps/client-next/tools/lib/performance-lease.mjs` | probe used only by profile-world |
| `apps/client-next/tests/runtime/performance-lease.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/profile-source-map.mjs` | a second source-map decoder; tools/perf/core/symbols.mjs is the one |
| `apps/client-next/tests/runtime/profile-source-map.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/release-profile.mjs` | profile build of the release for profile-world modes |
| `apps/client-next/tests/runtime/release-profile.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/status-notice-probe.mjs` | probe used only by profile-world |
| `apps/client-next/tools/lib/ui-product-probe.mjs` | probe used only by profile-world |
| `apps/client-next/tests/runtime/ui-product-probe.test.mjs` | test of a retired tool |
| `apps/client-next/tools/lib/ui-window-probe.mjs` | probe used only by profile-world |
