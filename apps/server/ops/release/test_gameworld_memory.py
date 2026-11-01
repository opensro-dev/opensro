"""
===========================================================================

test_gameworld_memory.py - the rendered GameWorld soft limit check (#2027)

The registered job is a fixture and the command runner a stand-in: no
test reaches Nomad.

===========================================================================
"""

import json
from types import SimpleNamespace
import unittest

from gameworld_memory import memory_limit_problems, verify_memory_limit
from test_deploy import rendered_job


# ================
# GameWorldMemoryTests
# ================
class GameWorldMemoryTests(unittest.TestCase):
	# ================
	# test_the_rendered_soft_limit_must_match_the_release
	#
	# The registered job's reservation and GOMEMLIMIT against the intended
	# memory: the live 1024/768MiB job fails a 3072 release, and so does a
	# job whose env formula drifted from three quarters.
	# ================
	def test_the_rendered_soft_limit_must_match_the_release(self):
		self.assertEqual(memory_limit_problems(rendered_job(3072), 3072), [])
		self.assertEqual(memory_limit_problems(rendered_job(3072)["Job"], 3072), [])
		self.assertEqual(len(memory_limit_problems(rendered_job(1024), 3072)), 2)
		self.assertEqual(memory_limit_problems(rendered_job(3072, "768MiB"), 3072),
			["gameworld GOMEMLIMIT is 768MiB, intended 2304MiB"])
		bare = {"Job": {"ID": "sro-gameworld-x", "TaskGroups": [{"Tasks": [{"Name": "gameworld", "Env": {}}]}]}}
		self.assertEqual(memory_limit_problems(bare, 3072), ["sro-gameworld-x renders no GOMEMLIMIT"])
		stale = SimpleNamespace(stdout=json.dumps(rendered_job(1024)))
		with self.assertRaisesRegex(RuntimeError, "768MiB, intended 2304MiB"):
			verify_memory_limit(["global-official"], {}, 3072, lambda *_a, **_o: stale)
		fresh = SimpleNamespace(stdout=json.dumps(rendered_job(3072)))
		verify_memory_limit(["global-official"], {}, 3072, lambda *_a, **_o: fresh)


if __name__ == "__main__":
	unittest.main()
