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
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import unittest.mock
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parents[2]
import rebuild_lock  # noqa: E402

LOCK_ENV = [
	"SRO_REBUILD_LOCK_NAME",
	"SRO_REBUILD_LOCK_TOKEN",
	"SRO_REBUILD_LOCK_DIR",
	"SRO_REBUILD_LOCK_MODE",
]

# Bounds for waits on child processes exiting. None of these carries a
# verdict - the state assertions do - so each only names a genuinely
# wedged child. 120 s matches the wait_for / waiting_marker deadlines: on
# a saturated box a correct child can take seconds to be scheduled, and a
# 30 s release wait fired as a failure under load (#2619).
WEDGED_CHILD_WAIT_S = 120

DRIVER = """
import os, sys
sys.path.insert(0, os.environ['SRO_TEST_SCRIPTS_DIR'])
from rebuild_lock import rebuild_lock
print('TRYING', flush=True)
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
			child.wait(timeout=WEDGED_CHILD_WAIT_S)
			if child.stdout:
				child.stdout.close()
		shutil.rmtree(self.lock_dir, ignore_errors=True)

	# A child expected to enter gets only a wedged-child bound: on a loaded
	# machine a correct lock can legitimately take seconds (a killed
	# contender's unpublished mutex claim has a 5 s grace), and an 800 ms
	# expiry turned that into a failure. Only the expiry test passes a short
	# timeout, because the expiry is what it tries.
	def start(self, mode: str, hold: bool = False, timeout_ms: int = 600000) -> subprocess.Popen:
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
		# A collector thread drains the child continuously: no read in this
		# harness may block past its own deadline the way readline() did,
		# and a child can never wedge on a full pipe.
		child.lines: list[str] = []

		def collect() -> None:
			for line in child.stdout:
				child.lines.append(line)

		child.collector = threading.Thread(target=collect, daemon=True)
		child.collector.start()
		self.children.append(child)
		return child

	def output(self, child: subprocess.Popen) -> str:
		return "".join(child.lines)

	def wait_for(self, child: subprocess.Popen, needle: str) -> None:
		# State, not a budget: the line either arrives, or the child ends
		# without it. The deadline only names a genuinely wedged child and
		# fires with everything it said attached.
		deadline = time.time() + 120
		while time.time() < deadline:
			if needle in self.output(child):
				return
			if child.poll() is not None:
				child.collector.join(timeout=10)
				self.fail(f"driver exited {child.returncode} before {needle}:\n{self.output(child)}")
			time.sleep(0.02)
		self.fail(f"driver never printed {needle}:\n{self.output(child)}")

	def entered(self, child: subprocess.Popen) -> None:
		self.wait_for(child, "ENTERED")

	def release(self, child: subprocess.Popen) -> None:
		child.stdin.write("\n")
		child.stdin.close()
		self.assertEqual(child.wait(timeout=WEDGED_CHILD_WAIT_S), 0)

	def waiting_marker(self, child: subprocess.Popen) -> None:
		# A writer publishes waiting/<token>.json before it queues: the
		# marker on disk is the queueing proof, as in the flagship below.
		waiting = self.lock_dir / "waiting"
		deadline = time.time() + 120
		while time.time() < deadline:
			if waiting.exists() and any(waiting.iterdir()):
				return
			if child.poll() is not None:
				child.collector.join(timeout=10)
				self.fail(f"writer exited {child.returncode} before queueing:\n{self.output(child)}")
			if "ENTERED" in self.output(child):
				self.fail(f"writer entered instead of queueing:\n{self.output(child)}")
			time.sleep(0.02)
		self.fail(f"writer never published its waiting marker:\n{self.output(child)}")

	def queued(self, mode: str) -> subprocess.Popen:
		# A contender whose refusal the harness proves from lock state:
		# its own lock timeout (600 s) is only a wedged-child bound the
		# pass path never approaches, never the verdict's clock.
		child = self.start(mode, hold=True, timeout_ms=600000)
		self.wait_for(child, "TRYING")
		if mode == "write":
			self.waiting_marker(child)
		else:
			# A negative-assertion window: a correct lock cannot print
			# ENTERED inside it however starved the machine is, whereas
			# the child's own 800 ms expiry also fired for a child that
			# had not been scheduled yet.
			end = time.time() + 5
			while time.time() < end:
				if "ENTERED" in self.output(child):
					self.fail(f"reader entered instead of queueing:\n{self.output(child)}")
				if child.poll() is not None:
					child.collector.join(timeout=10)
					self.fail(f"reader exited {child.returncode} instead of queueing:\n{self.output(child)}")
				time.sleep(0.02)
		self.assertIsNone(child.poll(), self.output(child))
		self.assertNotIn("ENTERED", self.output(child))
		return child

	def blocked(self, mode: str) -> None:
		child = self.queued(mode)
		child.kill()
		child.wait(timeout=WEDGED_CHILD_WAIT_S)

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
		writer.wait(timeout=WEDGED_CHILD_WAIT_S)

	def test_a_reader_queues_behind_a_waiting_writer(self) -> None:
		reader = self.start("read", hold=True)
		self.entered(reader)
		writer = self.start("write", hold=True, timeout_ms=600000)
		self.waiting_marker(writer)
		queued_reader = self.queued("read")
		self.release(reader)
		self.entered(writer)
		# Writer preference, positively: the queued reader is still out
		# while the writer holds, and is admitted once it lets go.
		self.assertNotIn("ENTERED", self.output(queued_reader))
		self.assertIsNone(queued_reader.poll(), self.output(queued_reader))
		self.release(writer)
		self.entered(queued_reader)
		self.release(queued_reader)

	def test_a_blocked_child_expires_with_its_timeout_line(self) -> None:
		# The expiry itself is the behaviour on trial here, so this one
		# child keeps a short lock timeout; its holder never lets go
		# during the test, which is what makes the expiry deterministic
		# however the machine is loaded.
		reader = self.start("read", hold=True)
		self.entered(reader)
		child = self.start("write", timeout_ms=800)
		self.assertNotEqual(child.wait(timeout=120), 0, self.output(child))
		child.collector.join(timeout=10)
		self.assertIn("timed out waiting", self.output(child))
		self.assertNotIn("ENTERED", self.output(child))
		self.release(reader)

	def test_a_killed_readers_claim_is_reaped_for_a_writer(self) -> None:
		reader = self.start("read", hold=True)
		self.entered(reader)
		self.blocked("write")
		reader.kill()
		reader.wait(timeout=WEDGED_CHILD_WAIT_S)
		writer = self.start("write")
		self.entered(writer)
		writer.wait(timeout=WEDGED_CHILD_WAIT_S)

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
			cwd=SCRIPTS_DIR.parent, env=env, capture_output=True, text=True, timeout=WEDGED_CHILD_WAIT_S,
		)
		self.assertEqual(run.returncode, 0, run.stderr)
		self.assertIn("REFUSED", run.stdout)
		self.assertIn("cannot be taken inside the read lock", run.stdout)
		self.assertNotIn("INNER WRITER ENTERED", run.stdout)
		self.assertIn("DONE", run.stdout)


class RebuildLockMutexRaceTest(unittest.TestCase):
	def test_state_mutex_retries_when_removal_lands_before_the_claim(self) -> None:
		# Deterministic injection, no timing: the first mutex claim's
		# mkdir removes the lock dir underneath itself, so it fails
		# with FileNotFoundError exactly as an empty-state release
		# landing between the ensure and the claim made it fail. The
		# retry must re-create the dir and still hand back the mutex.
		with tempfile.TemporaryDirectory() as tmp:
			lock_dir = Path(tmp) / "mutex-race.lock"
			mutex_dir = lock_dir / "mutex"
			real_mkdir = Path.mkdir
			injected = False

			def injecting_mkdir(self: Path, *args: object, **kwargs: object) -> None:
				nonlocal injected
				if self == mutex_dir and not injected:
					injected = True
					shutil.rmtree(lock_dir, ignore_errors=True)
				real_mkdir(self, *args, **kwargs)

			with unittest.mock.patch.object(Path, "mkdir", injecting_mkdir):
				token = rebuild_lock.acquire_state_mutex(lock_dir, None)
			self.assertTrue(injected)
			self.assertIsNotNone(token)
			owner = json.loads((mutex_dir / "owner.json").read_text(encoding="utf-8"))
			self.assertEqual(owner["token"], token)
			rebuild_lock.release_state_mutex(lock_dir, str(token))

	def test_state_mutex_release_outlasts_a_contender_reading_its_record(self) -> None:
		# A contender reads the mutex record at the moment of release. On
		# Windows that open handle refuses the delete; the release used to
		# swallow the failure and leave the mutex held under a live pid
		# for its 60 s staleness, stalling every contender. The release
		# must outlast the read and leave no mutex behind. Ordering is by
		# the child's own "open" line, not a timing guess.
		with tempfile.TemporaryDirectory() as tmp:
			lock_dir = Path(tmp) / "mutex-reader.lock"
			token = rebuild_lock.acquire_state_mutex(lock_dir, None)
			self.assertIsNotNone(token)
			reader = subprocess.Popen(
				[sys.executable, "-B", "-c",
					"import sys, time\n"
					"handle = open(sys.argv[1], encoding='utf-8')\n"
					"print('open', flush=True)\n"
					"time.sleep(0.5)\n"
					"handle.close()\n",
					str(lock_dir / "mutex" / "owner.json")],
				stdout=subprocess.PIPE, text=True,
			)
			try:
				self.assertEqual(reader.stdout.readline().strip(), "open")
				rebuild_lock.release_state_mutex(lock_dir, str(token))
			finally:
				reader.wait(timeout=WEDGED_CHILD_WAIT_S)
				reader.stdout.close()
			self.assertFalse((lock_dir / "mutex").exists(), "the released mutex is gone")
			# And it is free: the next claim succeeds at once.
			again = rebuild_lock.acquire_state_mutex(lock_dir, int(time.time() * 1000))
			self.assertIsNotNone(again)
			rebuild_lock.release_state_mutex(lock_dir, str(again))

	def test_an_unreadable_fresh_claim_still_counts_as_live(self) -> None:
		# A waiting writer's marker that cannot be read for a moment (its
		# owner is replacing it) is still a claim. Counted as absent, it
		# let a release see an idle lock and a new reader enter ahead of
		# the writer. The read failure is injected, not timed.
		with tempfile.TemporaryDirectory() as tmp:
			waiting = Path(tmp) / "unreadable.lock" / "waiting"
			waiting.mkdir(parents=True)
			(waiting / "writer-token.json").write_text("{}", encoding="utf-8")
			now_ms = int(time.time() * 1000)
			with unittest.mock.patch.object(rebuild_lock, "read_state_file", return_value=(now_ms, None)):
				live = rebuild_lock.inspect_claim_dir(waiting, 60000, reap=True)
			self.assertEqual(len(live), 1, "the unreadable claim blocks")
			self.assertIn("writer-token.json", str(live[0]["label"]))
			self.assertTrue((waiting / "writer-token.json").exists(), "a live claim is never reaped")

	def test_publish_retries_when_parent_removed_mid_publish(self) -> None:
		# The empty-state releases remove the whole state directory; a
		# publish landing inside that removal re-creates its parent and
		# publishes again instead of failing the claim it serves (#2820).
		# The removal is injected at the atomic replace, not timed.
		with tempfile.TemporaryDirectory() as tmp:
			target = Path(tmp) / "mutex" / "owner.json"
			record = {"token": "test-token", "pid": os.getpid()}
			real_replace = os.replace
			replaces = []

			def removing_replace(source: object, destination: object) -> None:
				replaces.append(1)
				if len(replaces) == 1:
					shutil.rmtree(Path(tmp) / "mutex")
				real_replace(source, destination)

			with unittest.mock.patch("os.replace", removing_replace):
				rebuild_lock.write_state_file(target, record)
			self.assertEqual(len(replaces), 2, "the publish retried after the removal")
			self.assertEqual(json.loads(target.read_text(encoding="utf-8"))["token"], "test-token")


if __name__ == "__main__":
	unittest.main()
