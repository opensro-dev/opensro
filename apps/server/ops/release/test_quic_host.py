"""
===========================================================================

test_quic_host.py - the UDP 443 redirect the public WebTransport needs

The rules, the unit, apply/remove and the readiness check, with every host
command captured: nothing here touches nftables, systemd or the firewall.

===========================================================================
"""

from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

import quic_host

READY_TABLE = ("table inet opensro_quic {\n\tchain prerouting {\n"
	"\t\ttype nat hook prerouting priority dstnat; policy accept;\n"
	"\t\tudp dport 443 redirect to :18443\n\t}\n}\n")
READY_UFW = ("Status: active\n\nTo                         Action      From\n"
	"--                         ------      ----\n"
	"18443/udp                  ALLOW       Anywhere                   # opensro WebTransport\n")


# ================
# FakeHost
#
# Answers the read-only commands problems() runs from a table of outputs.
# ================
class FakeHost:
	def __init__(self, nft=(0, READY_TABLE), enabled="enabled\n", ufw=READY_UFW):
		self.answers = {"nft": nft, "systemctl": (0, enabled), "ufw": (0, ufw)}

	def __call__(self, argv, **_):
		code, out = self.answers[argv[0]]
		return subprocess.CompletedProcess(argv, code, stdout=out, stderr="")


class QuicHostTest(unittest.TestCase):
	# ================
	# test_rules_redirect_443_to_the_bind_once
	#
	# One transaction: declare, delete, define. Reloading the file replaces
	# the rule instead of stacking a second one.
	# ================
	def test_rules_redirect_443_to_the_bind_once(self):
		rules = quic_host.redirect_rules(18443)
		lines = rules.splitlines()
		self.assertEqual(lines[1:3], ["table inet opensro_quic", "delete table inet opensro_quic"])
		self.assertIn("type nat hook prerouting priority dstnat; policy accept;", rules)
		self.assertEqual(rules.count("redirect to"), 1)
		self.assertIn("udp dport 443 redirect to :18443", rules)
		for port in (443, 80, 1023, 0, 65536, "18443"):
			with self.assertRaises(ValueError):
				quic_host.redirect_rules(port)

	# ================
	# test_unit_loads_and_unloads_the_table
	# ================
	def test_unit_loads_and_unloads_the_table(self):
		body = quic_host.REDIRECT_UNIT_BODY
		self.assertIn("ExecStart=/usr/sbin/nft -f /etc/opensro-release/quic-redirect.nft", body)
		self.assertIn("ExecStop=/usr/sbin/nft delete table inet opensro_quic", body)
		self.assertIn("RemainAfterExit=yes", body)
		self.assertIn("WantedBy=multi-user.target", body)

	# ================
	# test_apply_installs_loads_and_opens_the_bind_port
	# ================
	def test_apply_installs_loads_and_opens_the_bind_port(self):
		ran = []
		with patch.object(quic_host, "install_file") as installed, patch.object(quic_host, "quic_sysctls") as buffers:
			quic_host.apply(18443, run=lambda argv, **_: ran.append(argv))
		buffers.assert_called_once_with()
		written = {call.args[0]: call.args[1].decode() for call in installed.call_args_list}
		self.assertEqual(written, {quic_host.REDIRECT_RULES: quic_host.redirect_rules(18443),
			quic_host.REDIRECT_UNIT: quic_host.REDIRECT_UNIT_BODY})
		self.assertEqual(ran[:3], [["systemctl", "daemon-reload"],
			["systemctl", "enable", "opensro-quic-redirect.service"],
			["systemctl", "restart", "opensro-quic-redirect.service"]])
		self.assertEqual(ran[3][:3], ["ufw", "allow", "18443/udp"])
		with patch.object(quic_host, "install_file") as installed, patch.object(quic_host, "quic_sysctls"):
			with self.assertRaises(ValueError):
				quic_host.apply(443, run=lambda argv, **_: ran.append(argv))
		installed.assert_not_called()

	# ================
	# test_remove_undoes_the_redirect
	# ================
	def test_remove_undoes_the_redirect(self):
		ran = []
		with patch.object(quic_host, "REDIRECT_UNIT", Path("/nonexistent/unit")), \
				patch.object(quic_host, "REDIRECT_RULES", Path("/nonexistent/rules")):
			quic_host.remove(18443, run=lambda argv, **_: ran.append(argv))
		self.assertEqual(ran, [["systemctl", "disable", "--now", "opensro-quic-redirect.service"],
			["systemctl", "daemon-reload"], ["ufw", "delete", "allow", "18443/udp"]])

	# ================
	# test_problems_names_each_gap
	#
	# A ready host has no problems. Each missing piece is named on its own:
	# no live table, a redirect to another port, the unit off, the firewall
	# closed, default buffers.
	# ================
	def test_problems_names_each_gap(self):
		big = lambda path: "7500000\n"
		self.assertEqual(quic_host.problems(18443, run=FakeHost(), read=big), [])
		cases = {
			"no live nftables redirect": FakeHost(nft=(1, "Error: No such file or directory\n")),
			"redirect 'udp dport 443 redirect to :18443'": FakeHost(nft=(0, READY_TABLE.replace("18443", "19443"))),
			"is not enabled": FakeHost(enabled="disabled\n"),
			"does not allow 18443/udp": FakeHost(ufw="Status: active\n443/udp ALLOW Anywhere\n"),
		}
		for gap, host in cases.items():
			found = quic_host.problems(18443, run=host, read=big)
			self.assertEqual(len(found), 1, found)
			self.assertIn(gap, found[0])
		# An inactive firewall drops nothing, so it is no gap.
		self.assertEqual(quic_host.problems(18443, run=FakeHost(ufw="Status: inactive\n"), read=big), [])
		small = quic_host.problems(18443, run=FakeHost(), read=lambda path: "212992\n")
		self.assertEqual(small, ["rmem_max is 212992, below 7500000", "wmem_max is 212992, below 7500000"])


if __name__ == "__main__":
	unittest.main()
