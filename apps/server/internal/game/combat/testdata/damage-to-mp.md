# Damage-to-MP (Snow Shield) evidence

The v1.150 skill-data census has 22 `dgmp` rows: IDs 19592 through 19613,
covering Novice, Adept, Freeze and Intensify at 20-53 percent. Later book names
in the text catalog do not establish additional executable v1.150 rows.
Admission is by the complete timed program, not these IDs or names.

Native v1.188 GameServer evidence:

- `587630`, at `5880E5`: `dgmp` indexes one argument at descriptor `+4CC`.
- `594AC0`, at `594D28`: the active instance occupies manager `+1FC`.
- `5829D0`, at `582ABA`: retirement clears that contribution.
- `5A0B80`, at `5A0B87..5A0BA7`: zero total or both zero lane accumulators
  skip damage post-processing. Periodic abnormal HP debit does not call it.
- `5A13FE..5A14D8`: truncate the redirected fraction, subtract it from the
  HP hit, and charge trunc(redirected * 1.5) MP. If MP is short, add
  trunc((cost - current MP) / 1.5) back to HP and consume available MP.
  Each impact sees the MP remaining after its predecessor. No empty-MP
  retirement is requested here. Preserve the separate truncations, including
  one-point outcomes with zero MP.
- The server FPU control word is `027F` (53-bit precision), as in the prepared
  skill-cost path. Using an emulator's default extended precision changes
  boundary cases and is not the captured runtime environment.

`native-damage-to-mp.json` contains 2,304 original-machine cases, the executable
SHA-256 and FPU control word. Regenerate with Python, `pefile` and `unicorn`:

```text
python capture_damage_to_mp.py --binary /private/SR_GameServer.exe --output native-damage-to-mp.json
```

The harness executes original arithmetic and `CRT_ftol`, constructs the real
instance/context/descriptor chain, and stubs only the virtual MP getter/debit.
It asserts the block exit address so instruction-budget exhaustion cannot
masquerade as a result. The normal Go test needs only the committed corpus.

The implementation retains the existing casting-state, duration, cancellation,
death and logout owners. It freezes the percentage on the installed effect.
The client already understands the timed effect, tooltip parameter and MP-only
`33A6` combat refresh; the regression test proves that MP depletion does not
produce a second HP subtraction or environmental damage popup. No asset or
protocol change is needed.
