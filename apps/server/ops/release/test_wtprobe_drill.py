"""
===========================================================================

test_wtprobe_drill.py - the WebTransport-only break test's order and safety

A fake host answers the monitor states on a fake clock and records every
command: nothing here changes nftables, systemd or sends a page.

===========================================================================
"""

import unittest

import wtprobe_drill
from wtprobe_drill import FLEET_STATE, PRODUCTION_STATE, WTPROBE_STATE, Drill


# ================
# FakeHost
#
# The wtprobe monitor notifies "down" a while after the break and "up" a
# while after the restore; the fleet stays up unless told otherwise.
# ================
class FakeHost:
	def __init__(self, page_after=180, recover_after=150, fleet_breaks=False, never_pages=False):
		self.now, self.commands = 0.0, []
		self.broken_at = self.restored_at = None
		self.page_after, self.recover_after = page_after, recover_after
		self.fleet_breaks, self.never_pages = fleet_breaks, never_pages
		self.notified = "up"

	def run(self, argv, **options):
		self.commands.append(argv)
		if argv[:2] == ["/usr/sbin/nft", "-f"]:
			self.broken_at = self.now
			self.rules = options["input"]
		if argv[:3] == ["/usr/sbin/nft", "delete", "table"]:
			self.restored_at = self.now

	def read(self, path):
		if path == PRODUCTION_STATE:
			return {"operation": None}
		if path == FLEET_STATE:
			return {"phase": "down" if self.fleet_breaks and self.broken_at is not None else "up"}
		if self.notified == "up" and self.broken_at is not None and self.restored_at is None and 				not self.never_pages and self.now - self.broken_at >= self.page_after:
			self.notified = "down"
		if self.notified == "down" and self.restored_at is not None and self.now - self.restored_at >= self.recover_after:
			self.notified = "up"
		return {"phase": "up" if self.notified == "up" else "down", "notified": self.notified}

	def drill(self):
		return Drill(run=self.run, read=self.read, clock=lambda: self.now, sleep=self.advance)

	def advance(self, seconds):
		self.now += seconds


class WTProbeDrillTest(unittest.TestCase):
	# ================
	# test_the_break_drops_only_udp_443_before_the_redirect
	# ================
	def test_the_break_drops_only_udp_443_before_the_redirect(self):
		rules = wtprobe_drill.break_rules()
		self.assertIn("table inet opensro_wtdrill", rules)
		self.assertIn("type filter hook prerouting priority raw", rules)
		self.assertIn("udp dport 443 drop", rules)
		self.assertNotIn("tcp", rules)

	# ================
	# test_a_drill_pages_restores_and_recovers_in_order
	#
	# The fallback removal is armed before the break, and the table is
	# deleted before waiting for recovery.
	# ================
	def test_a_drill_pages_restores_and_recovers_in_order(self):
		host = FakeHost()
		report = host.drill().execute()
		self.assertGreaterEqual(report["paged"], 180)
		self.assertGreaterEqual(report["recovered"], 150)
		self.assertEqual(host.commands[0][0], "systemd-run")
		self.assertIn("--on-active=600", host.commands[0])
		self.assertEqual(host.commands[1][:2], ["/usr/sbin/nft", "-f"])
		self.assertEqual(host.commands[2], ["/usr/sbin/nft", "delete", "table", "inet", "opensro_wtdrill"])
		self.assertEqual(host.commands[3], ["systemctl", "stop", "opensro_wtdrill-fallback.timer"])

	# ================
	# test_no_page_still_restores_and_fails
	# ================
	def test_no_page_still_restores_and_fails(self):
		host = FakeHost(never_pages=True)
		with self.assertRaisesRegex(RuntimeError, "no 'down' notification"):
			host.drill().execute()
		self.assertIn(["/usr/sbin/nft", "delete", "table", "inet", "opensro_wtdrill"], host.commands)

	# ================
	# test_a_fleet_outage_aborts_and_restores
	#
	# If HTTPS goes down too, the drill broke more than WebTransport.
	# ================
	def test_a_fleet_outage_aborts_and_restores(self):
		host = FakeHost(fleet_breaks=True)
		with self.assertRaisesRegex(RuntimeError, "more than WebTransport"):
			host.drill().execute()
		self.assertIn(["/usr/sbin/nft", "delete", "table", "inet", "opensro_wtdrill"], host.commands)

	# ================
	# test_preflight_refuses_a_down_monitor_or_an_open_release
	# ================
	def test_preflight_refuses_a_down_monitor_or_an_open_release(self):
		for states in ({WTPROBE_STATE: {"phase": "down"}, FLEET_STATE: {"phase": "up"}, PRODUCTION_STATE: {"operation": None}},
				{WTPROBE_STATE: {"phase": "up"}, FLEET_STATE: {"phase": "up"}, PRODUCTION_STATE: {"operation": {"component": "server"}}}):
			commands = []
			drill = Drill(run=lambda argv, **_: commands.append(argv), read=lambda path, s=states: s[path], clock=lambda: 0, sleep=lambda _s: None)
			with self.subTest(states=states), self.assertRaises(RuntimeError):
				drill.execute()
			self.assertEqual(commands, [])


if __name__ == "__main__":
	unittest.main()
