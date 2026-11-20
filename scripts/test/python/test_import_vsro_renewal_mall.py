"""
test_import_vsro_renewal_mall.py - the renewal mall's published layout shape

parse_sections must read every Section of a resinfo file with its
controls in authored order; publish_control must hand the client the
Rect (texture-sized when the file leaves it zero), the published
texture under Media_extracted/later/, frames as prefixes, and colours
as [r,g,b,a] 0..1; picture_names must expand a frame into its eight
parts and a button into its state faces; layout_strings must take the
English column and only the keys asked for.
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

from import_vsro_renewal_mall import (
	layout_strings,
	parse_sections,
	picture_names,
	public_path,
	publish_control,
)

FIXTURE = (
	"Interface Text\r\n\r\n"
	'Section = Create,"0,0,0,0","0"\r\n{\r\n'
	"\tGDR_RENEWAL_ITEM_MALL_MAIN_BTN:CIFButton\r\n\t{\r\n"
	'\t\tClientRect=RECT,"0,0,0,0"\r\n'
	'\t\tDDJ=STRING,"interface\\\\mall\\\\mall_mainpage.ddj"\r\n'
	'\t\tFontColor=COLOR,"255,222,217,162"\r\n'
	'\t\tHAlign=INTEGER,"1"\r\n'
	'\t\tID=INTEGER,"104"\r\n'
	'\t\tRect=RECT,"18,45,0,0"\r\n'
	'\t\tText=STRING,""\r\n'
	"\t}\r\n"
	"\tGDR_RENEWAL_ITEM_MALL_LEFT_FRAME:CIFFrame\r\n\t{\r\n"
	'\t\tDDJ=STRING,"interface\\\\frame\\\\mall_ava_wnd_"\r\n'
	'\t\tID=INTEGER,"114"\r\n'
	'\t\tRect=RECT,"15,42,124,542"\r\n'
	"\t}\r\n}\r\n"
	'Section = MyInfo,"0,0,0,0","0"\r\n{\r\n'
	"\tGDR_ITEM_MALL_MYINFO:CIFNewItemMallMyInfo\r\n\t{\r\n"
	'\t\tDDJ=STRING,""\r\n'
	'\t\tID=INTEGER,"120"\r\n'
	'\t\tRect=RECT,"562,449,223,135"\r\n'
	'\t\tText=STRING,"UIIT_STT_SILKMALL_SILK"\r\n'
	"\t}\r\n}\r\n"
)


class RenewalMallImportTest(unittest.TestCase):
	def test_sections_in_authored_order(self):
		sections = parse_sections(FIXTURE)
		self.assertEqual(list(sections), ["Create", "MyInfo"])
		self.assertEqual(
			list(sections["Create"]),
			["GDR_RENEWAL_ITEM_MALL_MAIN_BTN", "GDR_RENEWAL_ITEM_MALL_LEFT_FRAME"],
		)
		self.assertEqual(sections["MyInfo"]["GDR_ITEM_MALL_MYINFO"]["class"], "CIFNewItemMallMyInfo")

	def test_zero_rect_takes_the_picture_size(self):
		button = parse_sections(FIXTURE)["Create"]["GDR_RENEWAL_ITEM_MALL_MAIN_BTN"]
		published = publish_control(
			"GDR_RENEWAL_ITEM_MALL_MAIN_BTN", button, {"interface/mall/mall_mainpage.ddj": [120, 28]}
		)
		self.assertEqual(published["rect"], [18, 45, 120, 28])
		self.assertEqual(published["id"], 104)
		self.assertEqual(published["texture"], "/assets/images/Media_extracted/later/interface/mall/mall_mainpage.png")
		self.assertFalse(published["frame"])
		self.assertEqual(published["fontColor"], [222 / 255, 217 / 255, 162 / 255, 1.0])
		self.assertEqual(published["hAlign"], 1)

	def test_frame_keeps_its_prefix_and_rect(self):
		frame = parse_sections(FIXTURE)["Create"]["GDR_RENEWAL_ITEM_MALL_LEFT_FRAME"]
		published = publish_control("GDR_RENEWAL_ITEM_MALL_LEFT_FRAME", frame, {})
		self.assertTrue(published["frame"])
		self.assertEqual(published["texture"], "/assets/images/Media_extracted/later/interface/frame/mall_ava_wnd_")
		self.assertEqual(published["rect"], [15, 42, 124, 542])

	def test_picture_names(self):
		self.assertEqual(len(picture_names("interface\\\\frame\\\\mall_wnd_")), 8)
		self.assertIn("interface/frame/mall_wnd_mid_up.ddj", picture_names("interface\\frame\\mall_wnd_"))
		self.assertEqual(
			picture_names("interface\\\\mall\\\\mall_button.ddj"),
			[
				"interface/mall/mall_button.ddj",
				"interface/mall/mall_button_focus.ddj",
				"interface/mall/mall_button_press.ddj",
				"interface/mall/mall_button_disable.ddj",
			],
		)

	def test_later_art_never_shadows_v1150(self):
		self.assertTrue(public_path("interface\\mall\\mall_tab_on.ddj").startswith(
			"/assets/images/Media_extracted/later/"
		))

	def test_layout_strings_take_english_for_wanted_keys(self):
		table = (
			"1\tUIIT_STT_SILKMALL_BUY_LIST\tkr\t\tzh\tcn\tde\tjp\tBuying list\n"
			"1\tUIIT_STT_UNUSED\tkr\t\tzh\tcn\tde\tjp\tUnused\n"
			"0\tUIIT_CTL_SILKMALL_INIT_ITEM\tkr\t\tzh\tcn\tde\tjp\tDisabled row\n"
		)
		self.assertEqual(
			layout_strings(table, ["UIIT_STT_SILKMALL_BUY_LIST", "UIIT_CTL_SILKMALL_INIT_ITEM"]),
			{"UIIT_STT_SILKMALL_BUY_LIST": "Buying list"},
		)


if __name__ == "__main__":
	unittest.main()
