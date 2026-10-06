# ===========================================================================
# rebuild_lock.py - shared generated asset ownership for Python publishers
#
# Uses the same physical-tree directory and owner protocol as rebuildLock.mjs.
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
DEFAULT_POLL_MS = 5000
DEFAULT_STALE_MS = 12 * 60 * 60 * 1000
HEARTBEAT_MS = 10000
OWNER_PUBLICATION_GRACE_MS = 60000


# ================
# generated_assets_lock
# ================
@contextmanager
def generated_assets_lock(label: str) -> Iterator[None]:
	with rebuild_lock(GENERATED_ASSETS_LOCK_NAME, label):
		yield


# ================
# rebuild_lock
# ================
@contextmanager
def rebuild_lock(name: str, label: str) -> Iterator[None]:
	normalized_name = normalize_lock_name(name)
	lock_dir = rebuild_lock_directory(normalized_name)
	if (os.environ.get(LOCK_NAME_ENV) == normalized_name and os.environ.get(LOCK_TOKEN_ENV)
			and os.environ.get(LOCK_DIR_ENV) == str(lock_dir)):
		yield
		return

	poll_ms = max(250, int_from_env("SRO_REBUILD_LOCK_POLL_MS", DEFAULT_POLL_MS))
	stale_ms = max(60000, int_from_env("SRO_REBUILD_LOCK_STALE_MS", DEFAULT_STALE_MS))
	timeout_ms = int_from_env("SRO_REBUILD_LOCK_TIMEOUT_MS", 0)
	owner_path = lock_dir / "owner.json"
	token = f"{os.getpid()}-{int(time.time() * 1000)}"
	started_at_ms = int(time.time() * 1000)
	next_notice_at_ms = 0

	lock_dir.parent.mkdir(parents=True, exist_ok=True)

	while True:
		try:
			lock_dir.mkdir()
			break
		except FileExistsError:
			owner = read_owner(owner_path)
			if remove_stale_lock_if_needed(lock_dir, owner, stale_ms, label):
				continue

			now_ms = int(time.time() * 1000)
			if now_ms >= next_notice_at_ms:
				print(
					f"[rebuild-lock] {label}: waiting for {format_owner(owner)} "
					f"({relative_lock_path(lock_dir)}).",
					file=sys.stderr,
					flush=True,
				)
				next_notice_at_ms = now_ms + max(poll_ms, 30000)

			if timeout_ms > 0 and now_ms - started_at_ms >= timeout_ms:
				raise TimeoutError(f"[rebuild-lock] {label}: timed out waiting for {format_owner(owner)}.")

			time.sleep(poll_ms / 1000)

	owner = owner_record(normalized_name, label, token, lock_dir)
	write_owner(owner_path, owner)
	stop_heartbeat = threading.Event()
	heartbeat = threading.Thread(
		target=heartbeat_owner,
		args=(owner_path, owner, stop_heartbeat),
		daemon=True,
	)
	heartbeat.start()

	os.environ[LOCK_NAME_ENV] = normalized_name
	os.environ[LOCK_TOKEN_ENV] = token
	os.environ[LOCK_DIR_ENV] = str(lock_dir)
	print(f"[rebuild-lock] {label}: acquired {relative_lock_path(lock_dir)} (pid {os.getpid()}).", file=sys.stderr)

	try:
		yield
	finally:
		stop_heartbeat.set()
		if os.environ.get(LOCK_TOKEN_ENV) == token:
			os.environ.pop(LOCK_NAME_ENV, None)
			os.environ.pop(LOCK_TOKEN_ENV, None)
			os.environ.pop(LOCK_DIR_ENV, None)
		shutil.rmtree(lock_dir, ignore_errors=True)
		print(f"[rebuild-lock] {label}: released {relative_lock_path(lock_dir)}.", file=sys.stderr)


# ================
# owner_record
# ================
def owner_record(name: str, label: str, token: str, lock_dir: Path) -> dict[str, object]:
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
	}


# ================
# heartbeat_owner
# ================
def heartbeat_owner(owner_path: Path, owner: dict[str, object], stop_event: threading.Event) -> None:
	while not stop_event.wait(HEARTBEAT_MS / 1000):
		updated = {**owner, "heartbeatAt": iso_now()}
		try:
			write_owner(owner_path, updated)
		except OSError:
			return


# ================
# write_owner
# ================
def write_owner(owner_path: Path, owner: dict[str, object]) -> None:
	# Publish complete metadata so contenders never read a partial heartbeat.
	temporary = owner_path.with_name(f"{owner_path.name}.{owner['token']}.{time.time_ns()}.tmp")
	try:
		temporary.write_text(json.dumps(owner, indent=2) + "\n", encoding="utf-8")
		os.replace(temporary, owner_path)
	finally:
		temporary.unlink(missing_ok=True)


# ================
# read_owner
# ================
def read_owner(owner_path: Path) -> dict[str, object] | None:
	try:
		return json.loads(owner_path.read_text(encoding="utf-8"))
	except (OSError, json.JSONDecodeError):
		return None


# ================
# remove_stale_lock_if_needed
# ================
def remove_stale_lock_if_needed(lock_dir: Path, owner: dict[str, object] | None, stale_ms: int, label: str) -> bool:
	# Directory creation claims the lock before owner.json is published.
	# A fresh directory with unreadable metadata is not an abandoned owner.
	if owner is None:
		try:
			if time.time() * 1000 - lock_dir.stat().st_mtime * 1000 < OWNER_PUBLICATION_GRACE_MS:
				return False
		except FileNotFoundError:
			return True
	pid = int(owner.get("pid", 0)) if owner and str(owner.get("pid", "")).isdigit() else 0
	heartbeat_at = str(owner.get("heartbeatAt") or owner.get("startedAt") or "") if owner else ""
	heartbeat_age_ms = int(time.time() * 1000) - parse_iso_ms(heartbeat_at)
	alive = process_alive(pid) if pid > 0 else False

	if alive and heartbeat_age_ms <= stale_ms:
		return False

	reason = f"stale heartbeat from pid {pid}" if alive else f"exited pid {pid or '(unknown)'}"
	print(f"[rebuild-lock] {label}: removing stale lock ({reason}).", file=sys.stderr, flush=True)
	shutil.rmtree(lock_dir, ignore_errors=True)
	return True


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
# ================
def relative_lock_path(lock_dir: Path) -> str:
	return Path(os.path.relpath(lock_dir, REBUILD_ROOT)).as_posix()


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
