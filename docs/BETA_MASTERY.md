# Beta mastery allowance

The game-world operator switch `SRO_BETA_MASTERY=on` gives both Chinese and
European characters a **5,000 total mastery-level budget**. This is an explicit
beta policy, not a native rule. Individual mastery ceilings, character-level
requirements, skill prerequisites and SP prices remain unchanged.

The Nomad game-world job enables it by default for the current beta through
`beta_mastery = "on"`. Localhost and production use this same job and code.
Directly launched game-world binaries retain native rules unless the environment
switch is enabled.

To return to native rules, put `off` in `<state-dir>/beta-mastery.txt`
(`apps/server/.state/cluster/beta-mastery.txt` with the default layout) and
deploy normally. The deployer passes this persistent setting to the Nomad
job's `beta_mastery` variable; it survives future releases. Put `on` in the
same file to re-enable beta limits. A missing file defaults to on for this beta;
an empty or invalid file refuses deployment. For a directly launched binary, unset
`SRO_BETA_MASTERY` or set it to `off`, then restart. Native total allowances
are Chinese **300** and European **min(2 × character level, 240)**.

The game world reads the switch once at startup and publishes its override
with every world-entry bootstrap. The browser skill window uses that value;
there is no separate client build flag. Missing configuration uses the existing
native display calculation, including level changes.

Changing the switch does **not** delete learned masteries, skills or spent SP.
A character above the native total can still enter the game, but further mastery
training is refused until the total is within the restored allowance. An official
launch character reset, if desired, is a separate operator decision.
