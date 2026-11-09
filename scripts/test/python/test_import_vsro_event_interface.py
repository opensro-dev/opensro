"""
test_import_vsro_event_interface.py - the later event layouts' published shape

parse_event_layout must hand the client exactly what
buildEventInterfaceLayout publishes for the native file: an Open of 0
closes, a missing Open opens, colors go from "a,r,g,b" to [r,g,b,a] 0..1,
and only the fields the file actually names are published.
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from import_vsro_event_interface import parse_event_layout

FIXTURE = (
	'Open = "1"\n'
	'TitleFont = "4"\n'
	'TitleColor = "255,169,152,21"\n'
	'TitlePosition = "31, 26"\n'
	'TitleString = "Welcome"\n'
	'TextFont = "0"\n'
	'TextColor = "255,98,89,20"\n'
	'TextPosition = "32, 67"\n'
	'TextMargin = "8"\n'
	'TextString = "First line\\nSecond line"\n'
	'PicBG = "interface\\event\\test_window.ddj"\n'
	'PicButton = "interface\\event\\test_close.ddj"\n'
)


class EventInterfaceTest(unittest.TestCase):
	def test_full_layout(self):
		layout = parse_event_layout(FIXTURE)
		self.assertTrue(layout["open"])
		self.assertEqual(layout["titleFont"], 4)
		self.assertEqual(
			layout["titleColor"], [169 / 255, 152 / 255, 21 / 255, 1.0]
		)
		self.assertEqual(layout["titlePosition"], [31, 26])
		self.assertEqual(layout["title"], "Welcome")
		self.assertEqual(layout["titleCode"], "")
		self.assertEqual(layout["textPosition"], [32, 67])
		self.assertEqual(layout["textMargin"], 8)
		self.assertEqual(layout["text"], "First line\nSecond line")
		self.assertEqual(
			layout["picBg"], "/assets/images/Media_extracted/interface/event/test_window.png"
		)
		self.assertEqual(
			layout["picButton"],
			"/assets/images/Media_extracted/interface/event/test_close.png",
		)
		self.assertNotIn("buttonRect", layout)

	def test_closed_and_defaults(self):
		layout = parse_event_layout('Open = "0"\nTitleStringCode = "SN_TEST"')
		self.assertFalse(layout["open"])
		self.assertEqual(layout["titleCode"], "SN_TEST")
		self.assertEqual(layout["textMargin"], 5)
		self.assertNotIn("picBg", layout)

	def test_open_absent_opens_and_button_rect(self):
		layout = parse_event_layout('ButtonPosition = "1,2,3,4"')
		self.assertTrue(layout["open"])
		self.assertEqual(layout["buttonRect"], [1, 2, 3, 4])


if __name__ == "__main__":
	unittest.main()
