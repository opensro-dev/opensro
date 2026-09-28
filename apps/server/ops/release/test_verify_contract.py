"""
===========================================================================

test_verify_contract.py - reject declared capabilities absent from the runtime.

This gate consumes runtime output, never source text. Renaming a version owner
does not weaken the required agreement between a built server and its release.

===========================================================================
"""

import unittest

from verify_contract import verify
from test_release_state import production


# ================
# VerifyContractTests
# ================
class VerifyContractTests(unittest.TestCase):
	# ================
	# test_exact_runtime_capabilities_are_accepted
	# ================
	def test_exact_runtime_capabilities_are_accepted(self):
		actual = production()["server"]["compatibility"]
		verify(actual, {"server": actual})

	# ================
	# test_claimed_extra_protocol_and_hidden_schema_change_are_rejected
	# ================
	def test_claimed_extra_protocol_and_hidden_schema_change_are_rejected(self):
		actual = production()["server"]["compatibility"]
		for key in ("protocolMax", "storeReadMax", "storeWrite"):
			declared = dict(actual)
			declared[key] += 1
			with self.subTest(key=key), self.assertRaisesRegex(ValueError, "compiled"):
				verify(actual, {"server": declared})


if __name__ == "__main__":
	unittest.main()
