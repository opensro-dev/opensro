"""
===========================================================================

arabia_quest_tables.py - the Arabia quests' rows and texts (#4134)

Port-only, not native. Baghdad's quests (82 QNO_AR_* rows, ids 1448 to
1764, levels 121 to 130) exist only in the later client; the continent
import (#3268) brought the town, its NPCs and their talk, but no quest
rows, so the NPCs offer nothing. This tool imports, from the v1.696
client:

	- the 82 quest rows (questdata.txt, whose index names its shards
	  there; no id is used in v1.150),
	- their contents rows (questcontentsdata.txt; the later table's
	  trailing columns the v1.150 layout lacks are dropped),
	- their game guide rows (gameguidedata.txt),
	- the quest strings (the later client's three textquest families,
	  sharded there and id-prefixed; merged into the one textquest.txt
	  the v1.150 client reads, English carried where the later client
	  ships it, authored here under the QUEST_ENGLISH contract where
	  it does not).

Not imported: refqusetreward.txt (a server table the v1.150 client
lacks; the quests' rewards ride the server's specs), and the 25 Arabia
quests that carry reward rows but no v1.696 questdata row (QNO_AR_071
to 090, QNO_AR_104 to 108): recorded on #4134, they join when their
identity rows source.

These are the continent's quests, so they follow its switch:
SRO_ARABIA, as one step of arabia_import's layer (add_quest_tables
takes that layer's reader, so the archives open once). Off, the tool
removes the rows and restores the replaced text lines, so a reused
variant cannot retain them. The server admits a quest spec only for a
quest whose row the build loaded, so off stays native there too. No
retail table is committed to Git.

===========================================================================
"""
import argparse
import json
import mmap
import os
import re
from pathlib import Path

import alexandria_map_tables as map_tables
import alexandria_npcs as characters
import cap110_blue_berserk as blue
import import_vsro_alexandria as tables
import later_tables
from prepare_client_resources import safe_target, write_atomically
from sro_paths import MAIN_CHECKOUT_ROOT

MARKER = ".opensro-arabia-quest-tables.json"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
FORMAT = "opensro-arabia-quest-tables-v1"
FLAG = "SRO_ARABIA"
SOURCE_ENV = "SRO_V1696_ROOT"
QUEST_CODE = re.compile(r"QNO_AR_")
TEXT_FILE = "textquest.txt"
TEXT_FAMILIES = ("textquest_queststring", "textquest_otherstring", "textquest_speech&name")
CONTENTS_COLUMNS = 13
# Table, the column naming a row, and whether a row belongs to the quests by
# its key or by any of its fields (the game guide names the quest in a symbol
# column).
QUEST_TABLE, CONTENTS_TABLE, GUIDE_TABLE = "questdata.txt", "questcontentsdata.txt", "gameguidedata.txt"
# The later client's help strings: the guide's region headers name theirs here.
HELP_TABLE = "texthelp.txt"
KEYS = {QUEST_TABLE: 2, CONTENTS_TABLE: 0, GUIDE_TABLE: 1}
ID_COLUMN = {QUEST_TABLE: 1}
# gameguidedata.txt: [enabled, id, Korean label, depth, parent or child
# count, content key, title key, quest code]. A depth-1 row is one quest's
# article; the client refuses one whose title or body has no text
# (quest-guide.ts), so the import does too.
GUIDE_DEPTH_COLUMN, GUIDE_PARENT_COLUMN, GUIDE_CONTENT_COLUMN, GUIDE_TITLE_COLUMN, GUIDE_QUEST_COLUMN = 3, 4, 5, 6, 7
# A composed guide body's section heads, as the shipped bodies write them
# (SN_PAYCON_QNO_AR_001): the Korean column and every other language.
GUIDE_HEAD = '<strong><font color="255,226,74,0">\u25c8 {}</font></strong><br>'
GUIDE_HEADS = {"korean": ("\uc784\ubb34", "\ud615\uc2dd"), "other": ("Task", "Format")}


# ================
# QUEST_ENGLISH
#
# Authored English for the quest lines the v1.696 client ships without
# one (no English cell in any of its language columns). The asset build
# refuses an active textquest row without English
# (englishCompletions.mjs, assertEnglishCompletionCoverage), so a key
# the source leaves blank and this table does not carry fails the
# build rather than shipping a Korean-only line. Under the talk
# catalog's contract (SPEECH_ENGLISH in arabia_import.py): each entry
# records the exact Korean it translates, applies only while the
# source has no English, and a changed Korean refuses the build as
# stale rather than showing an outdated line.
# (key: (Korean source, English)). Translated from the v1.696 Korean.
# ================
QUEST_ENGLISH = {
	"SN_PAY_QNO_AR_071": ("소원의 램프 1", "Lamp of Wishes 1"),
	"SN_PAY_QNO_AR_072": ("소원의 램프 2", "Lamp of Wishes 2"),
	"SN_PAY_QNO_AR_073": ("소원의 램프 3", "Lamp of Wishes 3"),
	"SN_PAY_QNO_AR_074": ("소원의 램프 4", "Lamp of Wishes 4"),
	"SN_PAY_QNO_AR_075": ("램프의 가디언 1", "Guardian of the Lamp 1"),
	"SN_PAY_QNO_AR_076": ("램프의 가디언 2", "Guardian of the Lamp 2"),
	"SN_PAY_QNO_AR_077": ("램프의 가디언 3", "Guardian of the Lamp 3"),
	"SN_PAY_QNO_AR_078": ("악의 여신 1", "Goddess of Evil 1"),
	"SN_PAY_QNO_AR_079": ("악의 여신 2", "Goddess of Evil 2"),
	"SN_PAY_QNO_AR_080": ("악의 여신 3", "Goddess of Evil 3"),
	"SN_PAY_QNO_AR_081": ("페트라 조사단", "The Petra Expedition"),
	"SN_PAY_QNO_AR_082": ("기괴한 생명체", "Grotesque Creatures"),
	"SN_PAY_QNO_AR_083": ("불타는 정령", "Burning Spirits"),
	"SN_PAY_QNO_AR_084": ("모래의 악마", "Demons of the Sand"),
	"SN_PAY_QNO_AR_085": ("수상한 꼭두각시", "Suspicious Puppets"),
	"SN_PAY_QNO_AR_086": ("사념의 덩어리", "Masses of Malice"),
	"SN_PAY_QNO_AR_087": ("화염의 지배자", "Lord of Flames"),
	"SN_PAY_QNO_AR_088": ("에메랄드 바다", "The Emerald Sea"),
	"SN_PAY_QNO_AR_089": ("램프의 샤바훈", "Shabahun of the Lamp"),
	"SN_PAY_QNO_AR_090": ("가디언 하이리쑨", "Guardian Hairisun"),
	"SN_PAY_QNO_AR_091": ("벌레는 질색이야", "I Hate Bugs"),
	"SN_PAY_QNO_AR_092": ("선인장을 조심해!", "Watch Out for Cacti!"),
	"SN_PAY_QNO_AR_093": ("사막에서 바늘 찾기", "A Needle in the Desert"),
	"SN_PAY_QNO_AR_094": ("식인 거미", "Man-Eating Spiders"),
	"SN_PAY_QNO_AR_095": ("선인장 뽑기", "Pulling Up Cacti"),
	"SN_PAY_QNO_AR_096": ("불타는 나무", "Burning Trees"),
	"SN_PAY_QNO_AR_097": ("불타는 해안", "The Burning Coast"),
	"SN_PAY_QNO_AR_098": ("왕국 조사단 지원", "Supporting the Royal Expedition"),
	"SN_PAY_QNO_AR_099": ("보급로 확보", "Securing the Supply Route"),
	"SN_PAY_QNO_AR_100": ("고급 크랩 요리", "Fine Crab Cuisine"),
	"SN_PAY_QNO_AR_101": ("특식 배달", "Special Meal Delivery"),
	"SN_PAY_QNO_AR_102": ("진로 확보", "Clearing the Way"),
	"SN_PAY_QNO_AR_103": ("진로 확보2", "Clearing the Way 2"),
	"SN_PAY_QNO_AR_104": ("정원의 애완동물", "Pets of the Garden"),
	"SN_PAY_QNO_AR_105": ("정원의 안방마님", "Mistress of the Garden"),
	"SN_PAY_QNO_AR_106": ("섬뜩한 뜨거움", "An Eerie Heat"),
	"SN_PAY_QNO_AR_107": ("뱀 조련사", "The Snake Tamer"),
	"SN_PAY_QNO_AR_108": ("마녀의 흔적", "Traces of the Witch"),
	# The lines of the twelve quests the dialog decision held back
	# (owner, 2026-11-26, #4134). Their rows ship as Korean-only stub
	# rows (4 columns, no English cell); project_later_row now carries
	# them. The six _01 offers are the client's own SN_PAYCON scenario
	# first paragraph, word for word (the Korean compared equal
	# against the archive). The six one-line greetings, and
	# QNO_AR_002's _02 accept line, which has no such twin, are
	# translated from the shipped Korean.
	"SN_TALK_QNO_AR_001_01": ("전 왕비에게 배신을 당했던 나는 그 충격에 차마 입에도 담기 힘든 끔찍한 일들을 저질러왔다네. 나의 새로운 왕비인 세라자드를 만나지 못했다면 나는 지금도 밤마다 왕국의 처녀들을 죽이는 일을 반복했을지도 모르지.", "After I was betrayed by the former queen, I committed terrible deeds that one can't even imagine.  Had I not met my new queen, Sheherazade, even now, I might be repeating the killing of the virgins of the kingdom every night."),
	"SN_TALK_QNO_AR_001_05": ("당신께 알라의 은총이 깃들기를..", "May the grace of Allah be upon you.."),
	"SN_TALK_QNO_AR_002_01": ("당신에 대한 이야기는 많이 들었습니다. 바다 건너에서 넘어오는 것은 바람만이 아니니까요.", "I have heard much about you.  Wind is not the only thing that travels over the sea."),
	"SN_TALK_QNO_AR_002_02": ("알라의 은총으로 왕께서 다시 눈 뜨셨지만 민심은 땅에 떨어지고 왕국의 치안은 엉망이 되었습니다. 거기에 국왕을 폐위시키고 권력을 찬탈하려는 무리들까지 나타나 이빨을 드러내고 있답니다.", "By Allah's grace, the king has opened his eyes again, but the hearts of the people have fallen and the kingdom's order lies in ruin. And now those who would cast down the king and seize power have appeared, baring their fangs."),
	"SN_TALK_QNO_AR_003_01": ("지금의 바그다드? 그야말로 태평성대가 아니겠는가. 국왕께선 온전한 정신을 되찾으시고 왕국을 뒤덮던 곡소리도 멎었으니 말이야. 하지만 그걸로 모든 것이 끝나는 것은 아니지. 어쨌거나 죽은 이들이 존재하고 누군가는 그들의 죽음에 대한 책임을 져야 할테니까. 뭐, 그런 마음들까지 모두 불온하다라고 몰아붙이는 건 억지가 아닐까 하는 생각이 드네. 오히려 나는 딸을 왕비의 자리에 올려놓고 국왕을 좌지우지하는 자들이 더 불온하고 불충한 무리들이 아닌가 생각한다네.", "Today's Baghdad? This place is really peaceful.  The king has fully recovered and the wailing that once covered the kingdom has ceased.  However, that does not settle everything.  The fact of the matter is, people died and somebody will have to take the blame for their death.  Some claim that such an attitude is a rebellious one, but I say such a claim is rather far-fetched.  On the contrary, if there is anyone who is rebellious and disloyal, it is the one who puts his daughter on the throne of the queen to wrap the king around his finger."),
	"SN_TALK_QNO_AR_004_01": ("내가 세라자드를 이용해서 국왕을 좌지우지하고 있다고? 그 고약한 여우가 그렇게 이야기하던가? 하! 웃기지도 않는 이야기로군. 아직 직접적인 물증이 나오지 않았을 뿐, 그가 반란을 도모하고 있다는 것은 공공연한 사실일세. 문제는 반란을 일으키려면 많은 수의 병력과 군자금이 필요할텐데 그것을 어디다 숨겼는지 그야말로 감쪽같다는 것이지. 최근에야 그나마 수상한 곳을 발견해 정보를 모으고 있는 중인데 쉽지가 않네.", "I am controlling the king through Sheherazade?  Is that what that crafty fox told you?  Ha!  What nonsense!  There may be no hard evidence yet, everyone knows that he is seeking rebellion.  To rebel, he needs many men and the funds for the war.  The trouble is, we know not where he is hiding them.  Recently, we have managed to discover a suspicious place and are gathering information.  But, this is no easy task."),
	"SN_TALK_QNO_AR_010_05": ("오, 정말로 약탈꾼을 물리쳐주셨군요. 알라여, 감사합니다!", "Oh, you really have driven off the marauders. Thank you, Allah!"),
	"SN_TALK_QNO_AR_012_01": ("이 농장 주위에는 수많은 늑대들이 호시탐탐 양을 노리고 배회하고 있습니다. 그 중에서도 포악한 아도니스놈들은 이름 그대로 너무 흉폭한지라 손도 쓰지 못하는 실정이지요. 언제까지 늑대들을 두려워하면서 살아가야 할런지..", "Many wolves are wandering around this farm, looking for a chance to attack our sheep.  Of these, the Violent Adonises are so violent - as their name implies - that we can't do anything about them.  How long do we have to live in fear of wolves?"),
	"SN_TALK_QNO_AR_014_05": ("아, 알라시여!", "Ah, Allah!"),
	"SN_TALK_QNO_AR_018_06": ("쟈밀룬이 이것을 제게..", "Jyamilrun gave this to me.."),
	"SN_TALK_QNO_AR_019_05": ("정지! 누구냐?", "Halt! Who goes there?"),
	"SN_TALK_QNO_AR_026_05": ("자네는…", "You are…"),
	"SN_TALK_QNO_AR_027_01": ("우리는 이 숲속에서 뭔가 수상한 자들이 발견되고 있다는 첩보를 조사하기 위해 파견되었다네. 하지만 갈색 갈기 쿨드라는 마물들이 숲을 막고 있어서 쉽사리 숲속으로 진군할 수 없는 실정이야. 원래 이 숲에는 이런 마물들은 없었는데.. 누군가가 인위적으로 풀어놓은 게 아닌가하는 의심마저 들 지경이라네.", "We are dispatched here to investigate the intelligence that suspicious people are found in the forest.  However, demons called Brown Mane Kulds are blocking the forest, impeding our advances into the forest.  There used to be no demons in the forest...  It makes me wonder whether someone released them intentionally."),
}


# ================
# enabled
#
# The continent's switch, as arabia_import.enabled reads it.
# ================
def enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# source_root
#
# The staged v1.696 client (SRO_V1696_ROOT, defaulting to inputs/sro_1696
# beside the game root), resolved as skytemple_import.source_root does.
# ================
def source_root(env=os.environ):
	configured = env.get(SOURCE_ENV, "").strip()
	if configured:
		return Path(configured)
	return MAIN_CHECKOUT_ROOT.parent / "inputs" / "sro_1696"


# ================
# is_quest_row
# ================
def is_quest_row(name, row):
	if name == GUIDE_TABLE:
		return bool(QUEST_CODE.search("\t".join(row)))
	return len(row) > KEYS[name] and bool(QUEST_CODE.search(row[KEYS[name]]))


# ================
# quest_rows
#
# The rows of the three tables to append: the later client's quest rows whose
# key the variant lacks, in the variant's layout. A quest id the variant uses
# for another quest refuses (none does today).
# ================
def quest_rows(later, variant):
	out = {}
	heads = guide_heads(later[GUIDE_TABLE])
	for name, key in KEYS.items():
		have = {row[key] for row in variant[name] if len(row) > key}
		chosen = [row for row in later[name] if (is_quest_row(name, row) or name == GUIDE_TABLE and row[key] in heads)
			and row[key] not in have]
		if name == CONTENTS_TABLE:
			chosen = [row[:CONTENTS_COLUMNS] for row in chosen]
		out[name] = chosen
	taken = {row[ID_COLUMN[QUEST_TABLE]] for row in variant[QUEST_TABLE] if len(row) > 1}
	clash = sorted(row[1] for row in out[QUEST_TABLE] if row[1] in taken)
	if clash:
		raise ValueError("a quest id is used by another quest in the variant: " + ", ".join(clash))
	if not out[QUEST_TABLE]:
		raise ValueError("the later client holds none of the Arabia quests")
	return out


# ================
# guide_heads
#
# The ids of the region headers (depth 0) that head an Arabia quest article.
# Five of them name no quest (300000 Baghdad, 310000 Kirk field, 320000
# Phantom Desert, 330000 Baghdad (Dungeon), 360000 Arabia Coast), so the
# quest-code filter alone left their articles orphaned and the client
# refused the whole guide at package time (r204h, "Orphan quest guide
# article", 82 tests).
# ================
def guide_heads(rows):
	parents = {row[GUIDE_PARENT_COLUMN] for row in rows
		if len(row) > GUIDE_QUEST_COLUMN and row[GUIDE_DEPTH_COLUMN] == "1" and is_quest_row(GUIDE_TABLE, row)}
	return {row[KEYS[GUIDE_TABLE]] for row in rows
		if len(row) > GUIDE_QUEST_COLUMN and row[GUIDE_DEPTH_COLUMN] == "0" and row[KEYS[GUIDE_TABLE]] in parents}


# ================
# head_help_rows
#
# The later help strings the imported region headers name, in the shape
# of the quest strings the import already fills.
# ================
def head_help_rows(read, guide):
	keys = {row[column] for row in guide if row[GUIDE_DEPTH_COLUMN] == "0"
		for column in (GUIDE_CONTENT_COLUMN, GUIDE_TITLE_COLUMN)}
	if not keys:
		return []
	rows = (map_tables.later_text_row(row) for row in read.rows(HELP_TABLE))
	return [row for row in rows if len(row) > blue.ENGLISH_COLUMN and row[blue.KEY_COLUMN] in keys]


# ================
# guide_rows
#
# The later client's guide rows without its strays: a row whose quest exists
# nowhere - no questdata row and no title text. v1.696 ships one: 370005
# names QNO_AR_109 with QNO_AR_108's label ("Trace of the Witch") under a
# parent that counts four children (370001..370004 are those four). The
# client refused it at package time (r204, "Missing quest article text"). A
# native data defect is not copied (owner, 2026-11-24). A row whose quest
# has text but no questdata row (071..090, 104..108 in v1.696) stays as
# before; any other missing text fails check_guide_text.
# ================
def guide_rows(rows, quests, texts):
	codes = {row[KEYS[QUEST_TABLE]] for row in quests if len(row) > KEYS[QUEST_TABLE]}
	return [row for row in rows if len(row) <= GUIDE_QUEST_COLUMN or not QUEST_CODE.search(row[GUIDE_QUEST_COLUMN])
		or row[GUIDE_QUEST_COLUMN] in codes or row[GUIDE_TITLE_COLUMN] in texts]


# ================
# composed_guide_bodies
#
# A text row for each quest article whose body v1.696 never wrote: 300002
# (QNO_AR_002) names SN_PAYCON_QNO_AR_002, which no textquest family holds,
# while the quest's own task lines (SN_CON_<code>_<n>) and format line
# (SN_NC_<code>) are there. The body is those two, in the shipped bodies'
# layout; reward, NPC and scenario stay in the quest window, which reads
# them from the quest itself. Port-only (owner, 2026-11-24: native data
# defects are fixed, not copied).
# ================
def composed_guide_bodies(guide, later_texts):
	by_key = {row[blue.KEY_COLUMN]: row for rows in later_texts for row in rows if len(row) > blue.KEY_COLUMN}
	out = []
	for row in guide:
		if len(row) <= GUIDE_QUEST_COLUMN or row[GUIDE_DEPTH_COLUMN] != "1" or row[GUIDE_CONTENT_COLUMN] in by_key:
			continue
		code = row[GUIDE_QUEST_COLUMN]
		prefix = "SN_CON_" + code + "_"
		steps = sorted(int(k[len(prefix):]) for k in by_key if k.startswith(prefix) and k[len(prefix):].isdigit())
		tasks = [by_key[f"{prefix}{step}"] for step in steps]
		form = by_key.get("SN_NC_" + code)
		if not tasks or form is None:
			continue
		body = list(form)
		body[blue.KEY_COLUMN] = row[GUIDE_CONTENT_COLUMN]
		for column in range(blue.KOREAN_COLUMN, len(body)):
			if not form[column]:
				continue
			task, shape = GUIDE_HEADS["korean" if column == blue.KOREAN_COLUMN else "other"]
			lines = "<br>".join(t[column] for t in tasks if len(t) > column)
			body[column] = ("<sml2>" + GUIDE_HEAD.format(task) + lines + "<br><br>" + GUIDE_HEAD.format(shape)
				+ form[column] + "</sml2>")
		out.append(body)
	return out


# ================
# check_guide_text
#
# Every quest article the import adds has a title, and a depth-1 article a
# body, with text: the client's own rule (quest-guide.ts), read with the
# importers' no-text rule (alexandria_npcs.is_placeholder_text), so a
# missing key fails here at import, not at the release's package step.
# ================
def check_guide_text(guide, texts, variant_text):
	missing = []
	for row in guide:
		if len(row) <= GUIDE_QUEST_COLUMN or not QUEST_CODE.search(row[GUIDE_QUEST_COLUMN]):
			continue
		keys = [row[GUIDE_TITLE_COLUMN]] + ([row[GUIDE_CONTENT_COLUMN]] if row[GUIDE_DEPTH_COLUMN] == "1" else [])
		for key in keys:
			english = texts.get(key)
			absent = key not in variant_text if english is None else characters.is_placeholder_text(english)
			if absent:
				missing.append(f"{row[KEYS[GUIDE_TABLE]]}:{key}")
	if missing:
		raise ValueError("Arabia guide rows name text the import does not carry: " + ", ".join(missing))


# ================
# project_later_row
#
# One later text row in the layout fill_text reads. A full row takes the
# shared projection. Two defect shapes in the archive name Arabia
# quests and took the length rule's path out of the build: the
# 4-column stub rows (key and Korean only), whose English QUEST_ENGLISH
# carries, become a blank-English row so complete_english fills them;
# a quest row a column or two short or long (SN_TALK_QNO_AR_026_03 at
# 15, SN_PAYCON_QNO_AR_002 at 18) is padded or cut to 17 first, so its
# English cell lands where the projection reads it. Any other short
# row keeps today's path, untouched.
#
# Only that slip, never more: SN_PAYCON_QNO_AR_071 is broken over
# three physical lines by raw newlines in its English text, so its
# first line is 10 columns ending mid-sentence ("Long time no see").
# Padded to 17 it shipped that cut text with its <font> and <sml2>
# left open, and every window that lays out quest guide text threw
# "Unbalanced guide markup" (r204n's client check, 88 tests). A row
# that far off is a fragment, not a row a cell short.
# ================
LATER_TEXT_COLUMNS = 17
MAX_COLUMN_SLIP = 2


def project_later_row(row):
	if len(row) == LATER_TEXT_COLUMNS:
		return map_tables.later_text_row(row)
	if len(row) > 3 and row[1].strip().isdigit() and QUEST_CODE.search(row[2]):
		if len(row) == 4:
			return [row[0], row[2], row[3]] + [""] * 12
		if abs(len(row) - LATER_TEXT_COLUMNS) <= MAX_COLUMN_SLIP:
			return map_tables.later_text_row((row + ["0"] * LATER_TEXT_COLUMNS)[:LATER_TEXT_COLUMNS])
	return row


# ================
# later_text_rows
#
# The three textquest families' rows, shards fanned out, each projected
# from the later 17-column layout into the layout fill_text reads.
# ================
def later_text_rows(read):
	return [[project_later_row(row) for row in read.rows(family + ".txt")]
		for family in TEXT_FAMILIES]


# ================
# text_symbols
#
# Every key of the later quest strings that names one of the quests, in
# file order.
# ================
def text_symbols(later_texts):
	found = []
	for rows in later_texts:
		found += [row[blue.KEY_COLUMN] for row in rows if len(row) > blue.ENGLISH_COLUMN + 1
			and QUEST_CODE.search(row[blue.KEY_COLUMN])]
	return tuple(dict.fromkeys(found))


# ================
# complete_english
#
# The later rows with QUEST_ENGLISH applied: a row whose English is a
# placeholder (characters.is_placeholder_text, the asset build's rule)
# takes its authored line (its recorded Korean must still match), a row
# with English keeps it, and a blank quest row the table does not carry
# is collected and raised, so the build refuses instead of shipping a
# Korean-only line.
# ================
def complete_english(rows):
	out, missing = [], []
	for row in rows:
		if len(row) <= blue.ENGLISH_COLUMN or not characters.is_placeholder_text(row[blue.ENGLISH_COLUMN]):
			out.append(row)
			continue
		key = row[blue.KEY_COLUMN]
		authored = QUEST_ENGLISH.get(key)
		if authored is None:
			if QUEST_CODE.search(key):
				missing.append(key)
			out.append(row)
			continue
		source, line = authored
		if source.strip() != row[blue.KOREAN_COLUMN].strip():
			raise ValueError(f"stale Arabia quest English for {key}: the v1.696 Korean changed")
		row = list(row)
		row[blue.ENGLISH_COLUMN] = line
		out.append(row)
	if missing:
		raise ValueError("the v1.696 client ships no English for: " + ", ".join(sorted(set(missing)))
			+ " (QUEST_ENGLISH does not carry them)")
	return out


# ================
# drop_rows
#
# The raw table without the lines whose key column is in owned. The table's
# own final line ending comes back as it was (a table without one keeps none),
# so the native bytes return exactly.
# ================
def drop_rows(raw, key_column, owned, ended):
	lines = tables.decode_table(raw).split("\r\n")
	kept = [line for line in lines if not (len(line.split("\t")) > key_column and line.split("\t")[key_column] in owned)]
	while kept and kept[-1] == "":
		kept.pop()
	return b"\xff\xfe" + ("\r\n".join(kept) + ("\r\n" if ended else "")).encode("utf-16-le")


# ================
# remove_owned
#
# What a previous on build left: the appended rows of the three tables, and
# the text (appended lines leave, replaced lines come back).
# ================
def remove_owned(text_root, prior):
	for name, keys in prior.get("rows", {}).items():
		path = text_root / name
		if path.exists():
			write_atomically(path, drop_rows(path.read_bytes(), KEYS[name], set(keys), prior["ended"][name]))
	record = prior.get("text")
	path = text_root / TEXT_FILE
	if record and path.exists():
		write_atomically(path, blue.restore_text(path.read_bytes(), set(record["appended"]), record["replaced"]))


# ================
# remove_quest_tables
#
# The layer's off path: the marker's record leaves, and so does it.
# ================
def remove_quest_tables(output):
	output = Path(output)
	marker = output / MARKER
	if marker.exists():
		remove_owned(output / TEXT_ROOT, json.loads(marker.read_text(encoding="utf-8")))
		marker.unlink()


# ================
# add_quest_tables
#
# read(name) returns one later textdata table of the v1.696 client (the
# Arabia layer's reader, over its already-open Media archive, whose
# sha256 archive_sha is).
# ================
def add_quest_tables(read, archive_sha, output):
	output = Path(output)
	text_root, marker = output / TEXT_ROOT, output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	if marker.exists():
		remove_owned(text_root, prior)
		marker.unlink()
	quests = read.rows(QUEST_TABLE)
	later_texts = [complete_english(rows) for rows in later_text_rows(read)]
	known = {row[blue.KEY_COLUMN] for rows in later_texts for row in rows if len(row) > blue.KEY_COLUMN}
	later = {QUEST_TABLE: quests, CONTENTS_TABLE: tables.table_rows(read(CONTENTS_TABLE)),
		GUIDE_TABLE: guide_rows(read.rows(GUIDE_TABLE), quests, known)}
	variant = {name: tables.table_rows(safe_target(text_root, name).read_bytes()) for name in KEYS}
	new_rows = quest_rows(later, variant)
	later_texts.append(composed_guide_bodies(new_rows[GUIDE_TABLE], later_texts))
	heads = head_help_rows(read, new_rows[GUIDE_TABLE])
	later_texts.append(heads)
	symbols = text_symbols(later_texts)
	symbols += tuple(row[blue.KEY_COLUMN] for row in heads if row[blue.KEY_COLUMN] not in symbols)
	by_key = {row[blue.KEY_COLUMN]: row for rows in later_texts for row in rows if len(row) > blue.ENGLISH_COLUMN + 1}
	text_path = safe_target(text_root, TEXT_FILE)
	variant_text = {row[blue.KEY_COLUMN] for row in tables.table_rows(text_path.read_bytes()) if len(row) > blue.KEY_COLUMN}
	check_guide_text(new_rows[GUIDE_TABLE], {k: row[blue.ENGLISH_COLUMN] for k, row in by_key.items()}, variant_text)
	raw, appended, replaced = blue.fill_text(text_path.read_bytes(), by_key, symbols)
	ended = {}
	for name, rows in new_rows.items():
		path = safe_target(text_root, name)
		ended[name] = tables.decode_table(path.read_bytes()).endswith("\n")
		write_atomically(path, characters.append_rows(path.read_bytes(), rows))
	write_atomically(text_path, raw)
	summary = {"enabled": True, "format": FORMAT, "portOnly": True,
		"quests": sorted(row[2] for row in new_rows[QUEST_TABLE]),
		"rows": {name: [row[KEYS[name]] for row in rows] for name, rows in new_rows.items()}, "ended": ended,
		"text": {"appended": appended, "replaced": replaced}, "textSymbols": len(symbols),
		"laterArchiveHash": archive_sha}
	write_atomically(marker, (json.dumps(summary, indent=2, ensure_ascii=False) + "\n").encode("utf-8"))
	return summary


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--v1696-root", default=None)
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	args = parser.parse_args()
	native, output = Path(args.native_extracted).resolve(), Path(args.output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("quest table overlay must be separate from native extraction")
	if not enabled():
		remove_quest_tables(output)
		print(json.dumps({"enabled": False}, indent=2))
		return
	root = Path(args.v1696_root) if args.v1696_root else source_root()
	with open(root / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		read = later_tables.Archive.media(media, index)

		summary = add_quest_tables(read, tables.sha256_of(media), output)
	print(json.dumps({key: value for key, value in summary.items() if key not in ("rows", "text", "quests", "ended")}, indent=2))


if __name__ == "__main__":
	main()
