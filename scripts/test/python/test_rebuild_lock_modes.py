"""
===========================================================================

test_rebuild_lock_modes.py - the Python lock is reader/writer too

Rebuild_lock.py shares its on-disk protocol with rebuildLock.mjs; the Node
harness (scripts/test/pipeline/generatedAssetLock.test.mjs) drives both
implementations through copied sources. These tests pin the Python API
surface itself - modes, writer preference, stale reaping, and the nested
no-op - with real child processes against unique lock names in the real
locks root (never the live "generated-assets" name), cleaned up after.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import os
import shutil
import subprocess
import sys
import time
import unittest
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(SCRIPTS_DIR))
import rebuild_lock  # noqa: E402

LOCK_ENV = [
	"SRO_REBUILD_LOCK_NAME",
	"SRO_REBUILD_LOCK_TOKEN",
	"SRO_REBUILD_LOCK_DIR",
	"SRO_REBUILD_LOCK_MODE",
]

DRIVER = """
import os, sys
sys.path.insert(0, os.environ['SRO_TEST_SCRIPTS_DIR'])
from rebuild_lock import rebuild_lock
with rebuild_lock(os.environ['SRO_TEST_LOCK_NAME'], 'python-mode-driver', mode=os.environ.get('MODE', 'write')):
    print('ENTERED', flush=True)
    if os.environ.get('HOLD') == '1':
        sys.stdin.readline()
"""


class RebuildLockModesTest(unittest.TestCase):
	def setUp(self) -> None:
		self.name = f"test-rw-modes-{os.getpid()}-{int(time.time() * 1000)}"
		self.lock_dir = rebuild_lock.LOCKS_ROOT / f"{self.name}.lock"
		self.children: list[subprocess.Popen] = []

	def tearDown(self) -> None:
		for child in self.children:
			if child.poll() is None:
				child.kill()
		for child in self.children:
			child.wait(timeout=30)
			if child.stdout:
				child.stdout.close()
		shutil.rmtree(self.lock_dir, ignore_errors=True)

	def start(self, mode: str, hold: bool = False, timeout_ms: int = 800) -> subprocess.Popen:
		env = {key: value for key, value in os.environ.items() if key not in LOCK_ENV}
		env.update({
			"SRO_TEST_SCRIPTS_DIR": str(SCRIPTS_DIR),
			"SRO_TEST_LOCK_NAME": self.name,
			"MODE": mode,
			"SRO_REBUILD_LOCK_TIMEOUT_MS": str(timeout_ms),
			"SRO_REBUILD_LOCK_POLL_MS": "100",
			"SRO_REBUILD_LOCK_STALE_MS": "60000",
		})
		if hold:
			env["HOLD"] = "1"
		child = subprocess.Popen(
			[sys.executable, "-B", "-c", DRIVER],
			cwd=SCRIPTS_DIR.parent, env=env,
			stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
			text=True,
		)
		self.children.append(child)
		return child

	def entered(self, child: subprocess.Popen) -> None:
		deadline = time.time() + 20
		while time.time() < deadline:
			if child.poll() is not None:
				self.fail(f"driver exited {child.returncode} before ENTERED:\n{child.stdout.read()}")
			line = child.stdout.readline()
			if "ENTERED" in line:
				return
		self.fail("driver never printed ENTERED")

	def release(self, child: subprocess.Popen) -> None:
		child.stdin.write("\n")
		child.stdin.close()
		self.assertEqual(child.wait(timeout=30), 0)

	def blocked(self, mode: str) -> None:
		child = self.start(mode)
		output, _ = child.communicate(timeout=60)
		self.assertNotEqual(child.returncode, 0, output)
		self.assertIn("timed out waiting", output)
		self.assertNotIn("ENTERED", output)

	def test_two_readers_hold_at_once(self) -> None:
		first = self.start("read", hold=True)
		self.entered(first)
		second = self.start("read", hold=True)
		self.entered(second)
		self.release(first)
		self.release(second)

	def test_a_writer_waits_for_a_reader_then_enters(self) -> None:
		reader = self.start("read", hold=True)
		self.entered(reader)
		self.blocked("write")
		self.release(reader)
		writer = self.start("write")
		self.entered(writer)
		writer.wait(timeout=30)

	def test_a_reader_queues_behind_a_waiting_writer(self) -> None:
		reader = self.start("read", hold=True)
		self.entered(reader)
		writer = self.start("write", hold=True, timeout_ms=30000)
		waiting = self.lock_dir / "waiting"
		deadline = time.time() + 20
		while time.time() < deadline:
			if waiting.exists() and any(waiting.iterdir()):
				break
		else:
			self.fail("writer never published its waiting marker")
		self.blocked("read")
		self.release(reader)
		self.entered(writer)
		self.blocked("read")
		self.release(writer)
		last = self.start("read")
		self.entered(last)
		last.wait(timeout=30)

	def test_a_killed_readers_claim_is_reaped_for_a_writer(self) -> None:
		reader = self.start("read", hold=True)
		self.entered(reader)
		self.blocked("write")
		reader.kill()
		reader.wait(timeout=30)
		writer = self.start("write")
		self.entered(writer)
		writer.wait(timeout=30)

	def test_a_writer_refuses_to_nest_inside_a_read_hold(self) -> None:
		script = """
import os, sys
sys.path.insert(0, os.environ['SRO_TEST_SCRIPTS_DIR'])
from rebuild_lock import rebuild_lock
name = os.environ['SRO_TEST_LOCK_NAME']
with rebuild_lock(name, 'outer-reader', mode='read'):
    with rebuild_lock(name, 'inner-reader', mode='read'):
        pass
    try:
        with rebuild_lock(name, 'inner-writer', mode='write'):
            print('INNER WRITER ENTERED', flush=True)
    except RuntimeError as error:
        print('REFUSED: %s' % error, flush=True)
print('DONE', flush=True)
"""
		env = {key: value for key, value in os.environ.items() if key not in LOCK_ENV}
		env.update({
			"SRO_TEST_SCRIPTS_DIR": str(SCRIPTS_DIR),
			"SRO_TEST_LOCK_NAME": self.name,
			"SRO_REBUILD_LOCK_TIMEOUT_MS": "800",
			"SRO_REBUILD_LOCK_POLL_MS": "100",
		})
		run = subprocess.run(
			[sys.executable, "-B", "-c", script],
			cwd=SCRIPTS_DIR.parent, env=env, capture_output=True, text=True, timeout=60,
		)
		self.assertEqual(run.returncode, 0, run.stderr)
		self.assertIn("REFUSED", run.stdout)
		self.assertIn("cannot be taken inside the read lock", run.stdout)
		self.assertNotIn("INNER WRITER ENTERED", run.stdout)
		self.assertIn("DONE", run.stdout)


if __name__ == "__main__":
	unittest.main()
