"""
===========================================================================

import_quest_text_evidence.py - snapshot the v1.150 quest text and SQL rewards

The v1.150 client is the content boundary for quests whose v1.150 script
did not survive. Its textquest.txt carries each mission's objective line
(SN_CON_*, with the count it asks for) and each quest's popup body
(SN_PAYCON_*, whose reward paragraph advertises EXP, skill EXP and gold).
Where the popup advertises no reward, the newer server's reward rows
(refqusetreward / refquestrewarditems) stand in, as they did for the
script-backed catalog (QNO_RM_OLDWOMAN_1).

Writes scripts/data/quest/v150-text-source.json and
scripts/data/quest/sql-rewards-source.json for the quests named by
compiled-quests-source.json. The build never reads the media or the
server backup.

===========================================================================
"""

import argparse
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / "scripts/data/quest"
TEXT_FORMAT = "sro-quest-v150-text-v1"
SQL_FORMAT = "sro-quest-sql-rewards-v2"

# The reward paragraph of a v1.150 popup opens with this heading.
REWARD_HEADING = "보상"


# ================
# read_table
# ================
def read_table(path):
	raw = path.read_bytes()
	if raw[:2] in (b"\xff\xfe", b"\xfe\xff"):
		text = raw.decode("utf-16")
	else:
		text = raw.decode("cp949")
	return raw, [line.split("\t") for line in text.splitlines() if line.strip()]


# ================
# plain
# ================
def plain(markup):
	return re.sub(r"\s+", " ", re.sub(r"<[^>]+>", " ", markup)).strip()


# ================
# advertised_rewards
#
# The numbers the popup's reward paragraph names: EXP (경험치), skill EXP
# (스킬 경험치) and gold (GOLD / 골드). A choice reward lists its scalars
# once per choice; they agree, so the first stands.
# ================
def advertised_rewards(body):
	start = body.find(REWARD_HEADING)
	if start < 0:
		return None
	paragraph = body[start + len(REWARD_HEADING):]
	# The paragraph ends where the scenario text resumes.
	paragraph = re.split(r"(관련 시나리오|相关剧情|剧情介绍|◈)", paragraph)[0]
	number = lambda pattern: (lambda m: int(m.group(1).replace(",", "")) if m else 0)(re.search(pattern, paragraph))
	return {
		"choices": "선택" in paragraph,
		"exp": number(r"(?<!스킬)(?<!스킬 )경험치\s*([\d,]+)"),
		"skillExp": number(r"스킬\s*경험치\s*([\d,]+)"),
		"gold": number(r"(?i)(?:GOLD|골드)\s*([\d,]+)"),
	}


# ================
# objective_count
#
# The count an objective line asks for: the number before 개 / 마리 /
# 명 (items, monsters, people), or none for a talk or capture line.
# ================
def objective_count(line):
	m = re.search(r"([\d,]+)\s*(?:개|마리|명|회)", line)
	return int(m.group(1).replace(",", "")) if m else None


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__.splitlines()[3])
	parser.add_argument("--textdata", required=True, help="v1.150 media textdata directory")
	parser.add_argument("--sql", required=True, help="server SR_GameRefData directory with refqusetreward.txt")
	args = parser.parse_args()
	quests = json.loads((DATA / "compiled-quests-source.json").read_text(encoding="utf-8"))["quests"]
	textdata, sql = Path(args.textdata), Path(args.sql)

	raw, rows = read_table(textdata / "textquest.txt")
	text = {row[1]: plain(row[2]) for row in rows if len(row) > 2 and row[0] == "1"}
	snapshot = {"format": TEXT_FORMAT, "textquestSHA256": hashlib.sha256(raw).hexdigest(), "quests": {}}
	for code in sorted(quests):
		symbols = {}
		for mission in quests[code]["missions"]:
			symbol = mission["fields"].get("0xd")
			if isinstance(symbol, str) and symbol in text:
				# Only the count is evidence; the media's prose stays out of git.
				symbols[symbol] = {"count": objective_count(text[symbol])}
		body = text.get("SN_PAYCON_" + code)
		snapshot["quests"][code] = {
			"objectives": symbols,
			"reward": advertised_rewards(body) if body else None,
			"symbols": sorted(s for s in text if code in s),
		}
	(DATA / "v150-text-source.json").write_text(json.dumps(snapshot, indent="\t", ensure_ascii=False) + "\n", encoding="utf-8", newline="\n")

	reward_raw, reward_rows = read_table(sql / "refqusetreward.txt")
	item_raw, item_rows = read_table(sql / "refquestrewarditems.txt")
	rewards = {"format": SQL_FORMAT, "refqusetrewardSHA256": hashlib.sha256(reward_raw).hexdigest(),
		"refquestrewarditemsSHA256": hashlib.sha256(item_raw).hexdigest(), "quests": {}}
	for row in reward_rows:
		if row[0] != "1" and not row[0].isdigit():
			continue
		code = row[1]
		if code not in quests:
			continue
		# refqusetreward columns: 0 QuestID, 1 CodeName, 2 IsView, 3
		# IsBasicReward, 4 IsItemReward, 5 IsCheckCondition, 6
		# IsCheckCountry, 7 IsCheckClass, 8 IsCheckGender, 10 Gold, 11 Exp,
		# 12 SPExp, 13 SP, 14 AP, 16 Hwan, 17 Inventory (slots added), 18
		# SelectionCnt: how many of the listed items the player picks. The
		# v1.188 server checks the client's pick count against it (51B370,
		# record +5, read by 6AD880); QNO_CH_SHAMAN_1 lists fourteen weapons
		# with 1 here.
		rewards["quests"][code] = {"gold": int(row[10]), "exp": int(row[11]), "skillExp": int(row[12]),
			"skillPoints": int(row[13]), "ap": int(row[14]), "hwan": int(row[16]), "inventorySlots": int(row[17]),
			"selectionCount": int(row[18]), "checkCountry": int(row[6]) != 0, "items": []}
	for row in item_rows:
		code = row[1]
		if code in rewards["quests"]:
			rewards["quests"][code]["items"].append({"choice": int(row[2]), "item": row[3], "count": int(row[7])})
	(DATA / "sql-rewards-source.json").write_text(json.dumps(rewards, indent="\t", ensure_ascii=False) + "\n", encoding="utf-8", newline="\n")


if __name__ == "__main__":
	main()
