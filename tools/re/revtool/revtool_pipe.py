"""
===========================================================================

revtool_pipe.py - the rizin process revtool talks to, never a spin

revtool used to open rizin through rzpipe 0.6.2 (open_sync). rzpipe
reads rizin's NUL-terminated answers in two loops, and both spin
forever at full CPU once rizin has exited:
  * at open, `while b"\x00" not in stdout.read(1024)` reads b"" from
    a dead rizin for ever;
  * per command, it first makes the pipe non-blocking, then retries an
    empty read with no wait (1 ms after an exception), so a terminator
    that never comes is polled forever.
On 2026-11-25 a revtool `xref` burned a core for 11 hours that way,
with no rizin process left. rzpipe's quit() also waits for an answer
`q` never writes.

This module owns rizin's process for every revtool caller (RizinSession,
seed apply). It starts `rizin -q0` itself and keeps the pipe BLOCKING:
a read sleeps in the kernel until rizin writes, and returns b"" the
moment rizin exits, so no wait ever polls and a dead rizin raises
RizinExited at once, naming the command. close() sends `q!` without
waiting for an answer, then kills a rizin that does not exit.

An object without a `process` (a test fake) keeps its own cmd(),
cmdj() and quit().

===========================================================================
"""

import json
import shutil
import subprocess

# How long close() lets rizin exit on `q!` before killing it.
QUIT_WAIT_SECONDS = 5.0

# One read's size: an `aflj` on the seeded client is megabytes.
READ_CHUNK = 1 << 16


class RizinExited(RuntimeError):
	"""rizin's process ended while revtool still needed an answer."""


# ================
# RizinPipe
#
# One rizin process. `process` is the field the functions below read; a
# test fake without it keeps its own methods.
# ================
class RizinPipe:

	def __init__(self, process):
		self.process = process

	def cmd(self, text):
		return command(self, text)

	def cmdj(self, text):
		return command_json(self, text)

	def quit(self):
		close(self)


# ================
# _read_answer
#
# Reads rizin's stdout up to the NUL that ends an answer. The pipe is
# blocking and unbuffered: each read returns what rizin has written, or
# b"" once rizin's end is closed, which is the only way an answer stops
# short. Bytes after the NUL never arrive: rizin writes one answer per
# command and waits for the next.
# ================
def _read_answer(proc, label):
	out = bytearray()
	while True:
		chunk = proc.stdout.read(READ_CHUNK)
		if not chunk:
			try:
				proc.wait(timeout=QUIT_WAIT_SECONDS)
			except subprocess.TimeoutExpired:
				pass
			raise RizinExited(f"rizin exited (code {proc.returncode}) during {label!r}")
		end = chunk.find(b"\0")
		if end >= 0:
			out += chunk[:end]
			return out.decode("utf-8", errors="ignore")
		out += chunk


# ================
# open
#
# Starts `rizin -q0 <binary>` (no other flags: an unknown one makes rizin
# print usage and exit).
# ================
def open(binary):
	rizin = shutil.which("rizin")
	if rizin is None:
		raise SystemExit("error: rizin is not on PATH (tools/re/revtool/README.md, quick start)")
	return start([rizin, "-q0", binary])


# ================
# start
#
# Starts a rizin-like process and reads past its startup banner, which
# ends in the first NUL. One that exits first (a binary it cannot
# load) raises RizinExited instead of being polled.
# ================
def start(argv):
	proc = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, bufsize=0)
	pipe = RizinPipe(proc)
	try:
		_read_answer(proc, "open " + str(argv[-1]))
	except RizinExited:
		close(pipe)
		raise
	return pipe


# ================
# _process
#
# The rizin process behind a pipe, or None for a test fake.
# ================
def _process(pipe):
	proc = getattr(pipe, "process", None)
	if isinstance(proc, subprocess.Popen):
		return proc
	return None


# ================
# command
#
# Sends one command and returns rizin's answer as text. Newlines become
# `;`, so a batch stays one round trip. Raises RizinExited when rizin is
# gone before or during the answer.
# ================
def command(pipe, text):
	proc = _process(pipe)
	if proc is None:
		return pipe.cmd(text) or ""
	stripped = text.strip()
	label = stripped.splitlines()[0][:60] if stripped else ""
	if proc.poll() is not None:
		raise RizinExited(f"rizin had exited (code {proc.returncode}) before {label!r}")
	try:
		proc.stdin.write((stripped.replace("\n", ";") + "\n").encode("utf-8"))
		proc.stdin.flush()
	except OSError as exc:
		raise RizinExited(f"rizin's stdin closed at {label!r}") from exc
	return _read_answer(proc, label)


# ================
# command_json
#
# command() parsed as JSON; None for an empty or unparsable answer, as
# callers expect (`or []`).
# ================
def command_json(pipe, text):
	if _process(pipe) is None:
		return pipe.cmdj(text)
	out = command(pipe, text).strip()
	if not out:
		return None
	try:
		return json.loads(out)
	except json.JSONDecodeError:
		return None


# ================
# close
#
# Ends the rizin process without waiting for an answer to `q!` (rizin
# writes none), killing it when it does not exit in time.
# ================
def close(pipe):
	proc = _process(pipe)
	if proc is None:
		quit_pipe = getattr(pipe, "quit", None)
		if quit_pipe is not None:
			try:
				quit_pipe()
			except Exception:
				pass
		return
	if proc.poll() is None:
		try:
			proc.stdin.write(b"q!\n")
			proc.stdin.flush()
		except OSError:
			pass
		try:
			proc.wait(timeout=QUIT_WAIT_SECONDS)
		except subprocess.TimeoutExpired:
			proc.kill()
			proc.wait()
	for stream in (proc.stdin, proc.stdout):
		try:
			if stream is not None:
				stream.close()
		except OSError:
			pass
