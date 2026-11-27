"""
===========================================================================

build_progress.py - named, timed spans and a heartbeat for the asset build

The one owner of build progress lines (#4337, blind spots B3/B4). r204l's
variant build went silent for minutes at a time: later_world_import ran its
navmesh, availability and table passes in quiet loops, and about 4.5 minutes
ran outside any step timer, so nobody could say what the time was spent on.

Every second of a build should sit inside a named span that prints when it
starts and how long it took, and a long span should say it is still alive.
This module holds three things:

- Phases: a clock over consecutive named phases inside one piece of work.
  enter() closes the previous phase with its time and opens the next; done()
  closes the last. Callers mark phase boundaries without re-indenting code.
- the current position (step and phase), the one state the heartbeat reads;
- Heartbeat: a daemon thread that prints the current position and its
  elapsed time every HEARTBEAT_SECONDS while a build runs, and says so when
  the build sits between steps.

Lines are flushed as they are printed, so a caller that pipes the build
never sees them late.

===========================================================================
"""

import threading
import time

# The longest silence a running build may keep: a span that outlives this
# prints a heartbeat line (owner 2026-11-25: every running step shows its
# phase and elapsed time at least every 30 s).
HEARTBEAT_SECONDS = 30.0

_lock = threading.Lock()
_position = {"step": None, "stepStarted": 0.0, "phase": None, "phaseStarted": 0.0, "after": None}


def _say(line: str) -> None:
	print(line, flush=True)


# ================
# begin_step / end_step
#
# The variant runner's step boundaries. Between end_step and the next
# begin_step the build is "between steps", and the heartbeat says after
# which one, so untimed work between steps is named too.
# ================
def begin_step(name: str) -> None:
	with _lock:
		_position.update(step=name, stepStarted=time.perf_counter(), phase=None, phaseStarted=0.0)


def end_step() -> None:
	with _lock:
		_position.update(after=_position["step"], step=None, phase=None, phaseStarted=0.0)


def _set_phase(name: str | None) -> None:
	with _lock:
		_position.update(phase=name, phaseStarted=time.perf_counter() if name else 0.0)


# ================
# position_line
#
# The build's current position as one line: the step and phase with their
# elapsed times, or the step it is waiting after.
# ================
def position_line(now: float | None = None) -> str:
	now = time.perf_counter() if now is None else now
	with _lock:
		step, phase, after = _position["step"], _position["phase"], _position["after"]
		step_elapsed = now - _position["stepStarted"]
		phase_elapsed = now - _position["phaseStarted"]
	if step is None:
		return f"build: between steps (after {after or 'start'})"
	line = f"step {step}: running {step_elapsed:.0f} s"
	if phase:
		line += f", phase {phase} {phase_elapsed:.0f} s"
	return line


class Phases:
	"""Consecutive named phases of one piece of work, each timed."""

	def __init__(self, label: str):
		self.label = label
		self.name: str | None = None
		self.started = 0.0

	# ================
	# enter
	#
	# Closes the open phase with its time and opens name.
	# ================
	def enter(self, name: str) -> None:
		self._close()
		self.name, self.started = name, time.perf_counter()
		_set_phase(f"{self.label}/{name}")
		_say(f"{self.label}: {name} ...")

	# ================
	# done
	#
	# Closes the open phase; the work is over.
	# ================
	def done(self) -> None:
		self._close()
		_set_phase(None)

	def _close(self) -> None:
		if self.name is not None:
			_say(f"{self.label}: {self.name} done in {time.perf_counter() - self.started:.1f} s")
			self.name = None


class Heartbeat:
	"""Prints position_line every interval until stopped."""

	def __init__(self, interval: float = HEARTBEAT_SECONDS, say=_say):
		self.interval = interval
		self.say = say
		self._stop = threading.Event()
		self._thread: threading.Thread | None = None

	def start(self) -> "Heartbeat":
		self._thread = threading.Thread(target=self._run, name="build-heartbeat", daemon=True)
		self._thread.start()
		return self

	def stop(self) -> None:
		self._stop.set()
		if self._thread is not None:
			self._thread.join()

	def _run(self) -> None:
		while not self._stop.wait(self.interval):
			self.say(position_line())

	def __enter__(self):
		return self.start()

	def __exit__(self, *_):
		self.stop()
