# Native evidence

The port follows the original game, so a claim about the original needs evidence you can point to.

- **review** Check a native claim in the disassembly. Decompiler output is a hypothesis, and function names in a
  database have been wrong. Port code is never evidence of native behaviour.
- **review** Search everything before saying something does not exist: the client's data files and text tables as well
  as the executable. Client strings are often UTF-16, so an ASCII search can miss them.
- **review** Label each unnamed function you read while tracing, and save the labels, so the next reader starts ahead.
- **review** Write-ups in `../research` can be out of date. Confirm a load-bearing fact before porting it.
- **review** A genuine native bug, such as a race or a write to the wrong item, is fixed rather than copied.
- **review** A PR that relies on native behaviour carries the native-claims table, and a reviewer re-checks each row.
- **review** Label each gameplay verdict as a bug, a native rule or tuning. Tuning is the owner's call.
