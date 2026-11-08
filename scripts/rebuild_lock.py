# ===========================================================================
# rebuild_lock.py - shared generated asset ownership for Python publishers
#
# Uses the same physical-tree directory and reader/writer protocol as
# rebuildLock.mjs: checks read the generated tree under a shared read hold,
# publishers hold it alone, and a waiting writer blocks new readers so a
# stream of checks cannot starve a build. See rebuildLock.mjs for the full
# protocol description; the on-disk shapes are identical.
# ===========================================================================

from __future__ import annotations

import json
import os
import shutil
import socket
import sys
import threading
import time
from calendar import timegm
from contextlib import contextmanager
from pathlib import Path
from typing import Iterator


from sro_paths import GENERATED_ROOT


REBUILD_ROOT = Path(__file__).resolve().parents[1]
LOCKS_ROOT = REBUILD_ROOT / ".state" / "locks"
GENERATED_ASSETS_LOCK_NAME = "generated-assets"

LOCK_NAME_ENV = "SRO_REBUILD_LOCK_NAME"
LOCK_TOKEN_ENV = "SRO_REBUILD_LOCK_TOKEN"
LOCK_DIR_ENV = "SRO_REBUILD_LOCK_DIR"
LOCK_MODE_ENV = "SRO_REBUILD_LOCK_MODE"
DEFAULT_POLL_MS = 5000
DEFAULT_STALE_MS = 12 * 60 * 60 * 1000
HEARTBEAT_MS = 10000
OWNER_PUBLICATION_GRACE_MS = 60000
# The state mutex is held for a handful of file operations, never across a
# wait, so a live holder older than this is stopped, not slow.
MUTEX_STALE_MS = 60000
MUTEX_SPIN_MS = 10000
MUTEX_SPIN_SLEEP_S = 0.015
# How long a removal keeps retrying a path another process holds open. A
# contender reads a record for well under a millisecond; a virus scanner
# can hold a fresh file for longer, so the bound is generous.
REMOVE_RETRY_MS = 10000
# How long a read retries a record that is present but cannot be opened: a
# contender's os.replace of that record holds it for a moment.
READ_RETRY_MS = 1000


# ================
# generated_assets_lock
# ================
@contextmanager
def generated_assets_lock(label: str, mode: str = "write") -> Iterator[None]:
	with rebuild_lock(GENERATED_ASSETS_LOCK_NAME, label, mode=mode):
		yield


# ================
# rebuild_lock
#
# Modes: "write" (the default) excludes everyone, as the lock always did;
# "read" shares with other readers. A nested acquisition (the
# SRO_REBUILD_LOCK_* env a lock wrapper hands its children) is a no-op,
# except a write inside a read hold, which fails loudly: the ancestor
# excludes nobody, so the nested writer could neither safely proceed nor
# safely wait for its own ancestor.
# ================
@contextmanager
def rebuild_lock(name: str, label: str, mode: str = "write") -> Iterator[None]:
	mode = normalize_mode(mode)
	normalized_name = normalize_lock_name(name)
	lock_dir = rebuild_lock_directory(normalized_name)
	if (os.environ.get(LOCK_NAME_ENV) == normalized_name and os.environ.get(LOCK_TOKEN_ENV)
			and os.environ.get(LOCK_DIR_ENV) == str(lock_dir)):
		held_mode = os.environ.get(LOCK_MODE_ENV, "write")
		if held_mode == "write" or mode == "read":
			yield
			return
		raise RuntimeError(
			f"[rebuild-lock] {label}: a write lock cannot be taken inside the read lock "
			"this process already holds; run it as its own top-level command."
		)

	poll_ms = max(250, int_from_env("SRO_REBUILD_LOCK_POLL_MS", DEFAULT_POLL_MS))
	stale_ms = max(60000, int_from_env("SRO_REBUILD_LOCK_STALE_MS", DEFAULT_STALE_MS))
	timeout_ms = int_from_env("SRO_REBUILD_LOCK_TIMEOUT_MS", 0)
	hold = acquire_rebuild_lock(normalized_name, label, mode, lock_dir, poll_ms, stale_ms, timeout_ms)

	os.environ[LOCK_NAME_ENV] = normalized_name
	os.environ[LOCK_TOKEN_ENV] = hold["token"]
	os.environ[LOCK_DIR_ENV] = str(lock_dir)
	os.environ[LOCK_MODE_ENV] = mode
	print(f"[rebuild-lock] {label}: acquired {relative_lock_path(lock_dir)} (pid {os.getpid()}, {mode}).", file=sys.stderr)

	try:
		yield
	finally:
		hold["stop_heartbeat"].set()
		release_rebuild_lock(hold, stale_ms)
		if os.environ.get(LOCK_TOKEN_ENV) == hold["token"]:
			os.environ.pop(LOCK_NAME_ENV, None)
			os.environ.pop(LOCK_TOKEN_ENV, None)
			os.environ.pop(LOCK_DIR_ENV, None)
			os.environ.pop(LOCK_MODE_ENV, None)
		print(f"[rebuild-lock] {label}: released {relative_lock_path(lock_dir)}.", file=sys.stderr)


# ================
# acquire_rebuild_lock
# ================
def acquire_rebuild_lock(name: str, label: str, mode: str, lock_dir: Path,
		poll_ms: int, stale_ms: int, timeout_ms: int) -> dict:
	owner_path = lock_dir / "owner.json"
	writer_path = lock_dir / "writer.json"
	token = f"{os.getpid()}-{int(time.time() * 1000)}"
	record = owner_record(name, label, token, lock_dir, mode)
	my_reader_path = lock_dir / "readers" / f"{token}.json"
	my_waiting_path = lock_dir / "waiting" / f"{token}.json"
	started_at_ms = int(time.time() * 1000)
	deadline_ms = started_at_ms + timeout_ms if timeout_ms > 0 else None
	next_notice_at_ms = 0
	waiting_published = False

	lock_dir.parent.mkdir(parents=True, exist_ok=True)

	while True:
		decision: dict = {}

		def decide(state: dict) -> None:
			nonlocal waiting_published
			if mode == "read":
				blocker = state["writer"] or (state["waiting"][0] if state["waiting"] else None)
				if blocker:
					decision["blocker"] = blocker
					return
				write_state_file(my_reader_path, {**record, "mode": "read"})
				write_state_file(owner_path, {**record, "mode": "read"})
				decision["acquired"] = True
				return
			# A writer publishes its intent before it waits, so readers that
			# arrive from this moment on queue behind it (writer preference).
			if not waiting_published:
				write_state_file(my_waiting_path, {**record, "mode": "waiting"})
				waiting_published = True
			blocker = state["writer"] or (state["readers"][0] if state["readers"] else None)
			if blocker:
				decision["blocker"] = blocker
				return
			remove_state_path(my_waiting_path)
			write_state_file(writer_path, {**record, "mode": "write"})
			write_state_file(owner_path, {**record, "mode": "write"})
			decision["acquired"] = True

		inspected = inspect_under_mutex(lock_dir, stale_ms, deadline_ms, decide)
		if inspected and decision.get("acquired"):
			stop_heartbeat = threading.Event()
			targets = [my_reader_path] if mode == "read" else [writer_path, owner_path]
			heartbeat = threading.Thread(
				target=heartbeat_claims,
				args=(targets, record, mode, owner_path, token, stop_heartbeat),
				daemon=True,
			)
			heartbeat.start()
			return {"token": token, "mode": mode, "lock_dir": lock_dir, "record": record,
					"my_reader_path": my_reader_path, "writer_path": writer_path,
					"owner_path": owner_path, "stop_heartbeat": stop_heartbeat}

		# A waiting writer keeps its intent fresh so contenders never reap it
		# as stale while it legitimately queues behind long readers.
		if mode == "write" and waiting_published:
			try:
				write_state_file(my_waiting_path, {**record, "mode": "waiting"})
			except OSError:
				pass

		now_ms = int(time.time() * 1000)
		if now_ms >= next_notice_at_ms:
			detail = format_owner(decision.get("blocker")) if inspected else "lock state to settle"
			print(
				f"[rebuild-lock] {label}: waiting for {detail} "
				f"({relative_lock_path(lock_dir)}).",
				file=sys.stderr,
				flush=True,
			)
			next_notice_at_ms = now_ms + max(poll_ms, 30000)

		if deadline_ms is not None and now_ms >= deadline_ms:
			if waiting_published:
				remove_state_path(my_waiting_path)
			blocker = decision.get("blocker")
			raise TimeoutError(f"[rebuild-lock] {label}: timed out waiting for {format_owner(blocker) if blocker is not None else 'another rebuild'}.")

		time.sleep(poll_ms / 1000)


# ================
# release_rebuild_lock
# ================
def release_rebuild_lock(hold: dict, stale_ms: int) -> None:
	lock_dir: Path = hold["lock_dir"]
	# Releasing must not give up: an unreleased claim with a live pid behind
	# it blocks writers until its heartbeat goes stale hours later. The
	# mutex always frees, so retry until it is taken.
	mutex_token = None
	while mutex_token is None:
		mutex_token = acquire_state_mutex(lock_dir, None)
		if mutex_token is None:
			time.sleep(MUTEX_SPIN_SLEEP_S)
	idle = False
	try:
		if hold["mode"] == "read":
			remove_state_path(hold["my_reader_path"])
		else:
			_, writer = read_state_file(hold["writer_path"])
			if writer is not None and writer.get("token") == hold["token"]:
				remove_state_path(hold["writer_path"])
			# The mirror goes before the inspection below: inspect_state
			# treats a mirror without its claim file as a writer (the
			# crashed-mid-release case), which would otherwise turn this
			# live release into a phantom hold.
			_, owner = read_state_file(hold["owner_path"])
			if owner is not None and owner.get("token") == hold["token"]:
				remove_state_path(hold["owner_path"])
		state = inspect_state(lock_dir, stale_ms)
		if not state["writer"] and state["readers"]:
			write_state_file(hold["owner_path"], {**state["readers"][0], "mode": "read"})
		elif not state["writer"]:
			remove_state_path(hold["owner_path"])
		if not state["writer"] and not state["readers"] and not state["waiting"]:
			# Fully idle: clear the state directory, the pre-reader/writer
			# observable. The mutex goes last, so no contender can claim
			# it while the rest is still being removed.
			for entry in list(lock_dir.iterdir()):
				if entry.name != "mutex":
					remove_state_path(entry)
			idle = True
	finally:
		release_state_mutex(lock_dir, mutex_token)
	if idle:
		# Contenders re-create the directory on demand; one that already
		# has leaves it non-empty, and then it stays.
		try:
			lock_dir.rmdir()
		except OSError:
			pass


# ================
# inspect_under_mutex
#
# Runs decide(state) with a freshly inspected state while the transition
# mutex is held. Returns False when the mutex stayed busy past its spin
# budget and the caller should treat the round as a wait.
# ================
def inspect_under_mutex(lock_dir: Path, stale_ms: int, deadline_ms: int | None, decide) -> bool:
	mutex_token = acquire_state_mutex(lock_dir, deadline_ms)
	if mutex_token is None:
		return False
	try:
		state = inspect_state(lock_dir, stale_ms)
		decide(state)
		return True
	finally:
		release_state_mutex(lock_dir, mutex_token)


# ================
# inspect_state
#
# Reads every claim in the state directory and reaps stale ones (unless
# reap is off). A record is stale when its pid is gone or its heartbeat is
# older than stale_ms; an unreadable record is stale only once it is older
# than the publication grace, so a claim whose metadata is mid-publish is
# never stolen. An owner.json without a mode is a legacy exclusive holder
# and is reported as the writer.
# ================
def inspect_state(lock_dir: Path, stale_ms: int, reap: bool = True) -> dict:
	lock_dir.mkdir(parents=True, exist_ok=True)
	state: dict = {"writer": None, "readers": [], "waiting": []}

	writer_path = lock_dir / "writer.json"
	mtime_ms, writer = read_state_file(writer_path)
	if mtime_ms is not None:
		if is_stale_record(writer, mtime_ms, stale_ms):
			if reap:
				remove_state_path(writer_path)
		elif writer is not None:
			state["writer"] = writer

	state["readers"] = inspect_claim_dir(lock_dir / "readers", stale_ms, reap)
	state["waiting"] = inspect_claim_dir(lock_dir / "waiting", stale_ms, reap)

	owner_path = lock_dir / "owner.json"
	mtime_ms, owner = read_state_file(owner_path)
	if mtime_ms is not None:
		mode = owner.get("mode") if owner else None
		if owner is None or mode is None or (mode == "write" and not state["writer"]):
			# A legacy holder (no mode), or a writer mirror whose claim
			# file is gone: either way it excludes everyone while live.
			if is_stale_record(owner, mtime_ms, stale_ms):
				if reap:
					remove_state_path(owner_path)
			elif owner is not None:
				state["writer"] = owner
	return state


# ================
# inspect_claim_dir
# ================
def inspect_claim_dir(directory: Path, stale_ms: int, reap: bool) -> list:
	try:
		names = sorted(entry.name for entry in directory.iterdir())
	except (FileNotFoundError, NotADirectoryError):
		return []
	live = []
	for name in names:
		if not name.endswith(".json"):
			continue
		mtime_ms, record = read_state_file(directory / name)
		if mtime_ms is None:
			continue
		if is_stale_record(record, mtime_ms, stale_ms):
			if reap:
				remove_state_path(directory / name)
		elif record is not None:
			live.append(record)
		else:
			# Present, inside its publication grace, and still unreadable:
			# a live claim. Dropping it let a release see an idle lock and
			# clear a waiting writer's marker, and a new reader entered
			# ahead of that writer.
			live.append(unreadable_claim(directory / name))
	return live


# ================
# unreadable_claim
#
# Stands in for a live claim whose record could not be read, so it still
# blocks and still names its file in the waiting notice.
# ================
def unreadable_claim(file_path: Path) -> dict[str, object]:
	return {"label": f"an unreadable claim ({file_path.name})", "token": "", "heartbeatAt": iso_now()}


# ================
# is_stale_record
# ================
def is_stale_record(record: dict | None, mtime_ms: int | None, stale_ms: int) -> bool:
	if record is None:
		if mtime_ms is None:
			return True
		return int(time.time() * 1000) - mtime_ms > OWNER_PUBLICATION_GRACE_MS
	pid_value = record.get("pid")
	try:
		pid = int(pid_value) if pid_value is not None else 0
	except (TypeError, ValueError):
		pid = 0
	if pid > 0 and not process_alive(pid):
		return True
	heartbeat_at = str(record.get("heartbeatAt") or record.get("startedAt") or "")
	heartbeat_age_ms = int(time.time() * 1000) - parse_iso_ms(heartbeat_at)
	return heartbeat_age_ms > stale_ms


# ================
# acquire_state_mutex
#
# os.mkdir is the claim; the owner record only publishes who holds it, for
# stale reaping. Returns the claim token, or None when the mutex stayed
# busy past its spin budget and the caller should treat the round as a
# wait. deadline_ms bounds the spin by the caller's overall deadline.
# ================
def acquire_state_mutex(lock_dir: Path, deadline_ms: int | None) -> str | None:
	mutex_dir = lock_dir / "mutex"
	mutex_owner_path = mutex_dir / "owner.json"
	token = f"{os.getpid()}-{int(time.time() * 1000)}-mutex"
	spin_until_ms = int(time.time() * 1000) + MUTEX_SPIN_MS
	if deadline_ms is not None:
		spin_until_ms = min(spin_until_ms, deadline_ms)

	while True:
		try:
			lock_dir.mkdir(parents=True, exist_ok=True)
		except FileExistsError:
			# Windows: an idle release's rmdir is still pending while a
			# handle is open on the directory; the name exists but is not
			# yet a directory again. The next round finds it gone.
			if int(time.time() * 1000) >= spin_until_ms:
				return None
			time.sleep(MUTEX_SPIN_SLEEP_S)
			continue
		try:
			mutex_dir.mkdir()
		except FileNotFoundError:
			# A release that emptied the state removed the lock dir
			# between the ensure above and this claim; the next round
			# re-creates it, still inside the spin deadline.
			if int(time.time() * 1000) >= spin_until_ms:
				return None
			time.sleep(MUTEX_SPIN_SLEEP_S)
			continue
		except FileExistsError:
			mtime_ms, owner = read_state_file(mutex_owner_path)
			if owner is not None and is_stale_record(owner, mtime_ms, MUTEX_STALE_MS):
				shutil.rmtree(mutex_dir, ignore_errors=True)
				continue
			if owner is None and mtime_ms is None:
				# A claim whose record is not yet published gets only a
				# brief grace: the record follows the mkdir immediately.
				try:
					if int(time.time() * 1000) - int(mutex_dir.stat().st_mtime * 1000) > 5000:
						shutil.rmtree(mutex_dir, ignore_errors=True)
						continue
				except FileNotFoundError:
					continue
			elif owner is None and is_stale_record(owner, mtime_ms, MUTEX_STALE_MS):
				shutil.rmtree(mutex_dir, ignore_errors=True)
				continue
			if int(time.time() * 1000) >= spin_until_ms:
				return None
			time.sleep(MUTEX_SPIN_SLEEP_S)
			continue

		record = owner_record("state mutex", "state mutex", token, lock_dir, "mutex")
		write_state_file(mutex_owner_path, record)
		return token


# ================
# release_state_mutex
#
# Removes the mutex only while its record still names this token: a claim
# reaped as stale and taken by someone else is theirs. The record is read
# again while it is present but unreadable, because skipping the removal on
# a failed read leaked the mutex to a live pid for MUTEX_STALE_MS.
# ================
def release_state_mutex(lock_dir: Path, token: str) -> None:
	mutex_dir = lock_dir / "mutex"
	give_up_at_ms = int(time.time() * 1000) + REMOVE_RETRY_MS
	while True:
		mtime_ms, owner = read_state_file(mutex_dir / "owner.json")
		if owner is not None or mtime_ms is None or int(time.time() * 1000) >= give_up_at_ms:
			break
		time.sleep(MUTEX_SPIN_SLEEP_S)
	if owner is not None and owner.get("token") == token:
		remove_state_path(mutex_dir)


# ================
# remove_state_path
#
# Windows refuses to delete a file another process has open, and Python's
# open() never grants delete sharing, so a contender reading a record at
# the moment of a release made the delete fail. rmtree(ignore_errors=True)
# then left the state mutex held under a live pid until it went stale
# (60 s); a plain unlink raised. Retry while the removal fails, and raise
# after REMOVE_RETRY_MS so a removal never fails silently. A removal that
# succeeds returns at once: the path may be re-created by its next owner.
# ================
def remove_state_path(path: Path) -> None:
	give_up_at_ms = int(time.time() * 1000) + REMOVE_RETRY_MS
	while True:
		try:
			if path.is_dir():
				shutil.rmtree(path)
			else:
				path.unlink()
			return
		except FileNotFoundError:
			# rmtree also raises this when an entry under it vanished;
			# what is left, if anything, is tried again.
			if not os.path.lexists(path):
				return
		except OSError:
			if int(time.time() * 1000) >= give_up_at_ms:
				raise
		time.sleep(MUTEX_SPIN_SLEEP_S)


# ================
# owner_record
# ================
def owner_record(name: str, label: str, token: str, lock_dir: Path, mode: str) -> dict[str, object]:
	now = iso_now()
	return {
		"name": name,
		"label": label,
		"token": token,
		"pid": os.getpid(),
		"ppid": os.getppid(),
		"user": os.environ.get("USERNAME") or os.environ.get("USER") or "",
		"host": socket.gethostname(),
		"cwd": os.getcwd(),
		"command": " ".join(sys.argv),
		"lockDir": str(lock_dir),
		"startedAt": now,
		"heartbeatAt": now,
		"mode": mode,
	}


# ================
# heartbeat_claims
# ================
def heartbeat_claims(targets: list, record: dict, mode: str, owner_path: Path, token: str,
		stop_event: threading.Event) -> None:
	while not stop_event.wait(HEARTBEAT_MS / 1000):
		for target in targets:
			try:
				write_state_file(target, {**record, "mode": mode, "heartbeatAt": iso_now()})
			except OSError:
				return
		if mode == "read":
			# Keep the owner.json summary fresh while it names this reader;
			# another reader's summary is theirs to maintain.
			try:
				_, summary = read_state_file(owner_path)
				if summary is not None and summary.get("token") == token and summary.get("mode") == "read":
					write_state_file(owner_path, {**summary, "heartbeatAt": iso_now()})
			except OSError:
				return


# ================
# write_state_file
#
# Atomic publish (write-temp-then-replace) so contenders never read a
# partial record, exactly as the single-owner protocol published
# owner.json. The empty-state releases remove the whole state directory,
# and a publish landing inside that removal re-creates its parent and
# tries again inside the retry budget instead of failing the claim it
# serves (the Node publisher retries the same window).
# ================
def write_state_file(file_path: Path, record: dict) -> None:
	give_up_at_ms = int(time.time() * 1000) + REMOVE_RETRY_MS
	while True:
		try:
			file_path.parent.mkdir(parents=True, exist_ok=True)
		except FileExistsError:
			# The claim directory's removal by an idle release is still
			# pending (see acquire_state_mutex).
			if int(time.time() * 1000) >= give_up_at_ms:
				raise
			time.sleep(MUTEX_SPIN_SLEEP_S)
			continue
		temporary = file_path.with_name(f"{file_path.name}.{record['token']}.{time.time_ns()}.tmp")
		try:
			temporary.write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
			os.replace(temporary, file_path)
			return
		except (FileNotFoundError, PermissionError):
			# The parent went away (or its Windows removal is still
			# pending) between the ensure above and this publish.
			if int(time.time() * 1000) >= give_up_at_ms:
				raise
			time.sleep(MUTEX_SPIN_SLEEP_S)
		finally:
			temporary.unlink(missing_ok=True)


# ================
# read_state_file
#
# Returns (mtime_ms, record); mtime_ms is None when the file is absent and
# record is None when the file is absent or unparseable.
# ================
def read_state_file(file_path: Path) -> tuple[int | None, dict | None]:
	give_up_at_ms = int(time.time() * 1000) + READ_RETRY_MS
	while True:
		try:
			mtime_ms = int(file_path.stat().st_mtime * 1000)
		except (FileNotFoundError, NotADirectoryError):
			return None, None
		try:
			return mtime_ms, json.loads(file_path.read_text(encoding="utf-8"))
		except FileNotFoundError:
			return None, None
		except json.JSONDecodeError:
			return mtime_ms, None
		except OSError:
			# Windows: a contender's os.replace of this record is in
			# flight. Records are published whole, so reading again
			# shortly finds the old or the new one.
			if int(time.time() * 1000) >= give_up_at_ms:
				return mtime_ms, None
			time.sleep(MUTEX_SPIN_SLEEP_S)


# ================
# read_owner
# ================
def read_owner(owner_path: Path) -> dict[str, object] | None:
	_, record = read_state_file(owner_path)
	return record


# ================
# process_alive
# ================
def process_alive(pid: int) -> bool:
	if sys.platform == "win32":
		return windows_process_alive(pid)
	try:
		os.kill(pid, 0)
		return True
	except PermissionError:
		return True
	except OSError:
		return False


# ================
# windows_process_alive
# ================
def windows_process_alive(pid: int) -> bool:
	# os.kill(pid, 0) is NOT a liveness probe on Windows: any signal other than
	# CTRL_C_EVENT/CTRL_BREAK_EVENT goes through TerminateProcess, so probing a
	# live lock holder would kill it (with exit code 0, no less). Query instead.
	import ctypes

	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	ERROR_ACCESS_DENIED = 5
	STILL_ACTIVE = 259

	kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
	handle = kernel32.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, False, pid)
	if not handle:
		# Access denied means the pid exists but belongs to another user.
		return ctypes.get_last_error() == ERROR_ACCESS_DENIED
	try:
		exit_code = ctypes.c_ulong()
		if not kernel32.GetExitCodeProcess(handle, ctypes.byref(exit_code)):
			return True
		return exit_code.value == STILL_ACTIVE
	finally:
		kernel32.CloseHandle(handle)


# ================
# format_owner
# ================
def format_owner(owner: dict[str, object] | None) -> str:
	if owner is None:
		return "another process with no owner metadata"
	pieces = [
		str(owner.get("label") or owner.get("name") or "another rebuild"),
		f"pid {owner['pid']}" if owner.get("pid") else "",
		str(owner.get("mode") or ""),
		f"{owner.get('user')}@{owner.get('host')}" if owner.get("user") and owner.get("host") else "",
		f"started {owner.get('startedAt')}" if owner.get("startedAt") else "",
	]
	command = str(owner.get("command") or "")
	command_suffix = f", command: {truncate(command, 180)}" if command else ""
	return f"{', '.join(piece for piece in pieces if piece)}{command_suffix}"


# ================
# normalize_lock_name
# ================
def normalize_lock_name(name: str) -> str:
	allowed = []
	for char in name.strip().lower():
		allowed.append(char if char.isalnum() or char in "._-" else "-")
	return "".join(allowed).strip("-") or "default"


# ================
# normalize_mode
# ================
def normalize_mode(mode: str) -> str:
	if mode in ("read", "write"):
		return mode
	raise RuntimeError(f'[rebuild-lock] unknown lock mode "{mode}" (expected "read" or "write").')


# ================
# rebuild_lock_directory
# ================
def rebuild_lock_directory(name: str) -> Path:
	root = LOCKS_ROOT
	if name == GENERATED_ASSETS_LOCK_NAME:
		# Beside the tree it guards, so SRO_GENERATED_ROOT and junctions agree with rebuildLock.mjs.
		generated = GENERATED_ROOT.resolve()
		root = generated.parent / ".state" / "locks"
	return root / f"{name}.lock"


# ================
# relative_lock_path
#
# The lock directory for log lines, relative to the checkout when it can be.
# A worktree on another drive than the shared lock has no relative path
# (os.path.relpath raises across Windows drives); print it absolute, as the
# JavaScript twin's path.relative does.
# ================
def relative_lock_path(lock_dir: Path) -> str:
	try:
		return Path(os.path.relpath(lock_dir, REBUILD_ROOT)).as_posix()
	except ValueError:
		return Path(lock_dir).as_posix()


# ================
# truncate
# ================
def truncate(value: str, max_length: int) -> str:
	return value if len(value) <= max_length else value[: max_length - 3] + "..."


# ================
# int_from_env
# ================
def int_from_env(name: str, fallback: int) -> int:
	try:
		return int(os.environ.get(name, ""))
	except ValueError:
		return fallback


# ================
# iso_now
# ================
def iso_now() -> str:
	return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


# ================
# parse_iso_ms
# ================
def parse_iso_ms(value: str) -> int:
	# Node writes `new Date().toISOString()` (millisecond precision, e.g.
	# 2026-07-28T12:00:00.123Z); Python writes second precision. Accept both,
	# with any number of fractional digits, or a live Node-held lock parses to
	# 0 and looks infinitely stale.
	if not value:
		return 0
	base = value
	millis = 0
	if value.endswith("Z") and "." in value:
		base, _, fraction = value[:-1].partition(".")
		base += "Z"
		if not fraction.isdigit():
			return 0
		millis = int((fraction + "000")[:3])
	try:
		return timegm(time.strptime(base, "%Y-%m-%dT%H:%M:%SZ")) * 1000 + millis
	except ValueError:
		return 0
