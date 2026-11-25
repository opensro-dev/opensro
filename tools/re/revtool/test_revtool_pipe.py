"""
===========================================================================

test_revtool_pipe.py - revtool_pipe against a real child process

A small Python child stands in for rizin: it writes a banner ending in
NUL, answers `echo <text>` with the text and a NUL terminator, like
`rizin -q0`, and exits without answering on `die`. The tests pin what
the 11-hour spin of 2026-11-25 lacked: a rizin that dies at startup or
mid-command raises at once and never spins, and close() returns even
though rizin answers nothing to `q!`.

	py -3 -m unittest tools.re.revtool.test_revtool_pipe

===========================================================================
"""

import os
import sys
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import revtool_pipe

# The stand-in rizin: one command per line, answers end in NUL.
FAKE_RIZIN = r"""
import sys
sys.stdout.buffer.write(b"banner\0")
sys.stdout.buffer.flush()
for line in sys.stdin.buffer:
	text = line.decode().strip()
	if text == "die":
		sys.exit(3)
	if text.startswith("q"):
		sys.exit(0)
	sys.stdout.buffer.write(text.removeprefix("echo ").encode() + b"\0")
	sys.stdout.buffer.flush()
"""

# A dead rizin must be noticed well inside this; the spin never ended.
DEATH_NOTICE_SECONDS = 5.0


# A rizin that cannot load its binary: it exits before the banner.
DEAD_RIZIN = "import sys; sys.exit(1)"


class PipeTests(unittest.TestCase):

	def setUp(self):
		self.pipe = revtool_pipe.start([sys.executable, "-I", "-c", FAKE_RIZIN])

	def tearDown(self):
		if self.pipe.process.poll() is None:
			self.pipe.process.kill()
		self.pipe.process.wait()
		for stream in (self.pipe.process.stdin, self.pipe.process.stdout):
			if not stream.closed:
				stream.close()

	def test_a_rizin_that_dies_at_startup_raises_instead_of_spinning(self):
		started = time.monotonic()
		with self.assertRaises(revtool_pipe.RizinExited):
			revtool_pipe.start([sys.executable, "-I", "-c", DEAD_RIZIN])
		self.assertLess(time.monotonic() - started, DEATH_NOTICE_SECONDS)

	def test_answers_up_to_the_terminator(self):
		self.assertEqual(revtool_pipe.command(self.pipe, "echo hello"), "hello")
		self.assertEqual(revtool_pipe.command_json(self.pipe, 'echo [1, 2]'), [1, 2])

	def test_a_rizin_that_dies_mid_command_raises_instead_of_spinning(self):
		started = time.monotonic()
		with self.assertRaises(revtool_pipe.RizinExited):
			revtool_pipe.command(self.pipe, "die")
		self.assertLess(time.monotonic() - started, DEATH_NOTICE_SECONDS)

	def test_a_command_after_rizin_exited_raises_at_once(self):
		with self.assertRaises(revtool_pipe.RizinExited):
			revtool_pipe.command(self.pipe, "die")
		with self.assertRaises(revtool_pipe.RizinExited):
			revtool_pipe.command(self.pipe, "echo late")

	def test_close_returns_without_an_answer_to_quit(self):
		started = time.monotonic()
		revtool_pipe.close(self.pipe)
		self.assertIsNotNone(self.pipe.process.poll())
		self.assertLess(time.monotonic() - started, DEATH_NOTICE_SECONDS)

	def test_a_pipe_without_a_process_keeps_its_own_commands(self):
		class Fake:
			def cmd(self, text):
				return "cmd:" + text

			def cmdj(self, text):
				return {"cmdj": text}

		self.assertEqual(revtool_pipe.command(Fake(), "aflj"), "cmd:aflj")
		self.assertEqual(revtool_pipe.command_json(Fake(), "aflj"), {"cmdj": "aflj"})


if __name__ == "__main__":
	unittest.main()
