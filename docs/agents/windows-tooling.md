# Windows tooling

- **gate** Never write source through Windows PowerShell 5.1: it emits the wrong encoding and CRLF.
- **review** Read a gate's own exit code. Piping it through `tail` or `grep` hides a failure.
- **review** Run one heavy gate at a time on a machine. The full check, type checks and full tests together can exhaust
  memory.
- **review** Do not run the whole browser test suite unattended; it launches many browsers at once.
- **review** In Git Bash, `go test -race` needs the MSYS2 compiler directory first on `PATH`.
- **review** Point `GOCACHE` and `GOTMPDIR` at a drive with room. Go test binaries run from the default temp path may be
  blocked by antivirus or by the loopback rules of a local firewall.
- **review** In PowerShell, `\"` is not an escape inside a double-quoted string. Use single quotes or a here-string.
- **review** Scripted merge-conflict resolution anchors markers to line starts, because banners contain `=====`.
