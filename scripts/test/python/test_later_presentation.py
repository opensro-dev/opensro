"""
===========================================================================

test_later_presentation.py - the imported skills' files and sound rules

Synthetic trees stand in for the native extraction and the variant, and a
fake later source for the v1.696 and VSRO archives: the pass joins the
rules and copies the files only the added rows name, records what no
source holds, and a rerun or an off build takes out what it wrote (#4730).

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import contextlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import later_presentation as presentation  # noqa: E402
import skytemple_import  # noqa: E402
from test_cap110_skill_effects import TREE, stage_row  # noqa: E402

INFO_ROW = "\t".join(["MOB_NEW", "MOB_NEW", "3.5"] + ["none"] * 9 + [""])
ADDED = TREE.replace("#section\tskillaniset2\r\n", INFO_ROW + "\r\n#section\tskillaniset2\r\n").replace(
	"#section\tskilleffectset\r\n", "1\tnew\tSKILL_C\tANI_SKILL_1\tFALSE\r\n#section\tskilleffectset\r\n") + (
	stage_row("SKILL_C", "READY", "skill_c_ready.efp") + "\r\n" + stage_row("SKILL_C", "SHOT", "absent.efp") + "\r\n")
NATIVE_SOUND = "\tMOB_OLD\tSND_STAND\t-\t-\t-\t-\t0\tCommon\\\told.wav\t80\r\n"
LATER_SOUND = "\r\n".join([
	"1\tMOB_NEW\tSND_STAND\t-\t-\t-\t-\t0\tmonster\\\tnew.wav\t80",
	"0\tMOB_NEW\tSND_DIE\t-\t-\t-\t-\t0\tmonster\\\toff.wav\t80",
	"1\tMOB_OLD\tSND_DIE\t-\t-\t-\t-\t0\tmonster\\\tolder.wav\t80",
	"1\tMOB_OTHER\tSND_STAND\t-\t-\t-\t-\t0\tmonster\\\tother.wav\t80",
])
HELD = {("Particles", "monster/arabia/skill_c_ready.efp"), ("Data", "prim/snd/monster/new.wav")}


# ================
# fake_source
# ================
@contextlib.contextmanager
def fake_source(root, pins):
	yield {"archives": {}, "sound": LATER_SOUND} if root else None


# ================
# fake_closure
#
# The walk: writes the seeds the fake archives hold into the variant.
# ================
def fake_closure(seeds, archives, native, output, tolerate_gaps=False):
	written = []
	for archive, path in seeds:
		if (archive, path) in HELD:
			relative = Path(presentation.FOLDERS[archive]) / path
			(output / relative).parent.mkdir(parents=True, exist_ok=True)
			(output / relative).write_bytes(path.encode())
			written.append(relative.as_posix())
	return {"files": written, "missingSounds": [], "missingEffects": [], "missingFiles": [], "unparsedFiles": []}


# ================
# PresentationTests
# ================
class PresentationTests(unittest.TestCase):
	# ================
	# trees
	# ================
	def trees(self, temp, effect_text):
		native, out = Path(temp) / "native", Path(temp) / "out"
		for root, text in ((native, TREE), (out, effect_text)):
			(root / presentation.TEXT_ROOT).mkdir(parents=True)
			(root / presentation.TEXT_ROOT / "skilleffect.txt").write_bytes(b"\xff\xfe" + text.encode("utf-16-le"))
		(native / presentation.TEXT_ROOT / presentation.SOUND_TABLE).write_bytes(NATIVE_SOUND.encode("cp949"))
		return native, out

	# ================
	# run_pass
	# ================
	def run_pass(self, native, out):
		with mock.patch.object(presentation, "later_source", fake_source), \
			mock.patch.object(skytemple_import, "copy_resource_closure", fake_closure):
			return presentation.add_presentation(native, out, (("later", None), (None, None)))

	# ================
	# test_added_rows_get_their_rules_and_files_and_leave_off
	# ================
	def test_added_rows_get_their_rules_and_files_and_leave_off(self):
		with tempfile.TemporaryDirectory() as temp:
			native, out = self.trees(temp, ADDED)
			summary = self.run_pass(native, out)

			sounds = (out / presentation.TEXT_ROOT / presentation.SOUND_TABLE).read_bytes().decode("cp949")
			# The added kind's live rule joins in the native layout; a
			# switched-off rule, a native kind's and another kind's stay out.
			self.assertIn("\tMOB_NEW\tSND_STAND\t-\t-\t-\t-\t0\tmonster\\\tnew.wav\t80", sounds.split("\r\n"))
			self.assertNotIn("off.wav", sounds)
			self.assertNotIn("older.wav", sounds)
			self.assertNotIn("other.wav", sounds)
			self.assertTrue(sounds.startswith(NATIVE_SOUND))
			self.assertTrue((out / "Particles_extracted/monster/arabia/skill_c_ready.efp").is_file())
			self.assertTrue((out / "Data_extracted/prim/snd/monster/new.wav").is_file())
			# A file no source holds is recorded, never invented.
			self.assertEqual(summary["missing"], ["Particles:monster/arabia/absent.efp"])
			self.assertEqual(summary["files"], 2)
			# The native skill's file is not the import's.
			marker = json.loads((out / presentation.MARKER).read_text(encoding="utf-8"))
			self.assertNotIn("native.efp", json.dumps(marker))

			# A rerun answers the same.
			self.assertEqual(self.run_pass(native, out), summary)
			self.assertEqual((out / presentation.TEXT_ROOT / presentation.SOUND_TABLE).read_bytes().decode("cp949"), sounds)

			# Off: the imports wrote nothing, so the pass takes its own out.
			(out / presentation.TEXT_ROOT / "skilleffect.txt").write_bytes(b"\xff\xfe" + TREE.encode("utf-16-le"))
			self.assertEqual(self.run_pass(native, out), {"enabled": False})
			self.assertEqual((out / presentation.TEXT_ROOT / presentation.SOUND_TABLE).read_bytes().decode("cp949"), NATIVE_SOUND)
			self.assertFalse((out / "Particles_extracted/monster/arabia/skill_c_ready.efp").exists())
			self.assertFalse((out / presentation.MARKER).exists())

	# ================
	# test_an_earlier_source_answers_first
	# ================
	def test_an_earlier_source_answers_first(self):
		first = "1\tMOB_NEW\tSND_STAND\t-\t-\t-\t-\t0\tv1696\\\tnew.wav\t80"
		second = "1\tMOB_NEW\tSND_STAND\t-\t-\t-\t-\t0\tvsro\\\tnew.wav\t80\r\n1\tSKILL_C\tSND_X\tSKILL_C\t-\t-\t-\t0\tvsro\\\tc.wav\t80"
		rules = presentation.join_sound_rules(NATIVE_SOUND, [first, second], {"MOB_NEW"}, {"SKILL_C"})
		self.assertEqual(len(rules), 2)
		self.assertIn("v1696", rules[0])
		self.assertIn("c.wav", rules[1])
		self.assertEqual(presentation.drop_sound_rules(NATIVE_SOUND + "\r\n".join(rules) + "\r\n", rules), NATIVE_SOUND)


if __name__ == "__main__":
	unittest.main()
