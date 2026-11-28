"""
===========================================================================

arabia_people.py - Baghdad's people: where its NPCs stand, what they say

PORT-ONLY, NOT NATIVE. Split out of arabia_import.py (SRO_ARABIA) on its
own responsibility boundary: the continent import keeps the land, the
kinds and the driver; this module keeps the people - the NPC placements
(the authored rows first, then the standings the client's own npcpos
names) and the talk rows that ride them (the later client's speech
shards, with the authored English for the lines it ships Korean-only).

arabia_import re-exports the three functions, so its callers and tests
keep one namespace.

===========================================================================
"""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
sys.path.insert(0, str(Path(__file__).resolve().parent / "build"))
import alexandria_npcs as characters  # noqa: E402


# ================
# npc_placements
#
# Where the continent's NPCs stand: the extraction's authored rows
# first (they win a shared place), then the standings the client's own
# npcpos names inside the continent that the extraction does not.
# Returns [(codename, region, x, y, z)] in a stable order.
# ================
def npc_placements(npc_rows, npcpos_rows, codename_by_id, regions):
	found, seen = [], set()
	for row in npc_rows:
		if not row["Codename"].startswith("NPC_"):
			continue
		key = (row["Codename"], int(row["RegionID"]), round(float(row["X"])), round(float(row["Z"])))
		seen.add(key)
		found.append((row["Codename"], int(row["RegionID"]),
			float(row["X"]), float(row["Y"]), float(row["Z"])))
	for row in npcpos_rows:
		if len(row) < 5 or row[0] not in codename_by_id:
			continue
		code = codename_by_id[row[0]]
		if not code.startswith("NPC_"):
			continue
		try:
			region, x, y, z = int(row[1]) & 0xFFFF, float(row[2]), float(row[3]), float(row[4])
		except ValueError:
			continue
		if region not in regions:
			continue
		key = (code, region, round(x), round(z))
		if key in seen:
			continue
		seen.add(key)
		found.append((code, region, x, y, z))
	return sorted(found, key=lambda place: (place[0], place[1], place[2], place[4]))


# ================
# SPEECH_ENGLISH
#
# Authored English for the talk lines the v1.696 client ships in Korean
# only (no English cell in any of its language columns). The asset
# build refuses an active textquest row without English
# (englishCompletions.mjs, assertEnglishCompletionCoverage), and the
# shared catalog cannot hold these keys: it must name only rows the
# native build has, and these exist only while SRO_ARABIA is on. So the
# layer that adds the rows carries their English, under the catalog's
# contract: each entry records the exact Korean it translates, applies
# only while the source has no English, and a changed Korean refuses
# the build as stale rather than showing an outdated line.
# (key: (Korean source, English)). Translated from the v1.696 Korean,
# 2026-11-15, freeze 2c9e87fdf's variant build.
# ================
SPEECH_ENGLISH = {
	"SN_BOSS_DUNGEON_ALCHEMIST_BS": (
		"저는 오랜 연구 끝에 마침내 유피테르 신전에 숨겨져 있다는 비밀의 방의 입구를 열 수 있었습니다. "
		"하지만 전 입구만을 열 수 있을 뿐.. 비밀의 방이 어떤 곳인지 알지 못합니다.",
		"After long research I was finally able to open the entrance to the secret chamber said to be "
		"hidden in the Jupiter Temple. But I could only open the entrance.. I do not know what kind of "
		"place the secret chamber is.",
	),
	"SN_LAST_FIELD_PRIEST_BS": (
		"샴발라에 다녀는 와 봤나? 최근 많은 모험가들이 샴발라를 다녀온 후로  무언가에 홀리듯 사라졌다 "
		"나타나곤 했지. 그리고 얼마 지나지 않아 대도시를 중심으로 역병이 창궐하고 있다네.",
		"Have you ever been to Shambala? Lately many adventurers who came back from Shambala would vanish, "
		"as if bewitched by something, and then turn up again. And not long after, a plague broke out, "
		"spreading from the great cities.",
	),
	"SN_LOST_TOMB_DUNGEON_ALCHEMIST_BS": (
		"유피테르 신전에 숨겨져 있는 또 다른 입구를 찾아냈습니다. 이곳은 상층, 하층으로 구성된 무덤으로 "
		"파악이 됐으나  굳게 잠겨져 있어 조사가 중단됐습니다.",
		"I have found another entrance hidden in the Jupiter Temple. It proved to be a tomb with an upper "
		"and a lower level, but it is firmly locked, so the survey has stopped.",
	),
}


# ================
# speech_english
#
# A talk row's English: the source's own when it has one, else the
# authored line (SPEECH_ENGLISH) for that key and Korean, else "".
# ================
def speech_english(key, korean, english):
	if english:
		return english
	authored = SPEECH_ENGLISH.get(key)
	if authored is None:
		return ""
	source, line = authored
	if source.strip() != korean.strip():
		raise ValueError(f"stale Arabia talk English for {key}: the v1.696 Korean changed")
	return line


# ================
# later_speech_texts
#
# The later client's NPC talk strings (the SN_..._BS/_PS keys its
# npcchat rows name), normalised to the row shape later_object_texts
# returns for the name table. The later client keeps them in its
# textquest_speech&name shards; the native client keeps its own talk
# rows in textquest.txt itself.
# ================
def later_speech_texts(read):
	rows = []
	for row in read.rows("textquest_speech&name.txt", characters.translation_rows, indexed_only=True):
		# An untranslated row keeps only its first cells (the table
		# reader trims the empty tail); it still rides, with no
		# English, like the names that lack one.
		if len(row) < 3 or not row[2]:
			continue
		english = next((row[at] for at in (9, 13, 14, 15, 16)
			if len(row) > at and row[at] not in ("0", "")), "")
		korean = row[3] if len(row) > 3 else ""
		rows.append([row[0], row[2], korean,
			"", "", "", "", "", speech_english(row[2], korean, english), ""])
	return rows

