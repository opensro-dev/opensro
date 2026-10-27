"""
===========================================================================

cap110_skill_database.py - project an owner-supplied SQL skill snapshot

Port-only, not native. The SQL _RefSkill column order matches the 118-column
client layout, but JSON object order is not a schema. Read explicit field
names and retain database identities until the existing codename remapper
joins them to the already-published client tier. Never execute snapshot data.

===========================================================================
"""
import json
from pathlib import Path

import cap110_skills as skills
import sro_pk2

# One of three switches that must agree (#1746): SRO_CAP110_SKILLS
# publishes the client tier, this snapshot adds the 101-110 database
# tier, and the server's SRO_LEVEL_CAP admits the levels.
DATABASE_ENV = "SRO_CAP110_SKILL_DATABASE"
DATABASE_COLUMNS = (
	"Service ID GroupID Basic_Code Basic_Name Basic_Group Basic_Original Basic_Level Basic_Activity "
	"Basic_ChainCode Basic_RecycleCost Action_PreparingTime Action_CastingTime Action_ActionDuration "
	"Action_ReuseDelay Action_CoolTime Action_FlyingSpeed Action_Interruptable Action_Overlap "
	"Action_AutoAttackType Action_InTown Action_Range Target_Required TargetType_Animal TargetType_Land "
	"TargetType_Building TargetGroup_Self TargetGroup_Ally TargetGroup_Party TargetGroup_Enemy_M "
	"TargetGroup_Enemy_P TargetGroup_Neutral TargetGroup_DontCare TargetEtc_SelectDeadBody "
	"ReqCommon_Mastery1 ReqCommon_Mastery2 ReqCommon_MasteryLevel1 ReqCommon_MasteryLevel2 "
	"ReqCommon_Str ReqCommon_Int ReqLearn_Skill1 ReqLearn_Skill2 ReqLearn_Skill3 ReqLearn_SkillLevel1 "
	"ReqLearn_SkillLevel2 ReqLearn_SkillLevel3 ReqLearn_SP ReqLearn_Race Req_Restriction1 Req_Restriction2 "
	"ReqCast_Weapon1 ReqCast_Weapon2 Consume_HP Consume_MP Consume_HPRatio Consume_MPRatio Consume_WHAN "
	"UI_SkillTab UI_SkillPage UI_SkillColumn UI_SkillRow UI_IconFile UI_SkillName UI_SkillToolTip "
	"UI_SkillToolTip_Desc UI_SkillStudy_Desc AI_AttackChance AI_SkillType"
).split() + [f"Param{index}" for index in range(1, 51)]
MAX_MASTERY = 110


# ================
# project_database_rows
# ================
def project_database_rows(records):
	if not isinstance(records, list):
		raise ValueError("skill snapshot must be an array of _RefSkill records")
	rows = []
	for record in records:
		if not isinstance(record, dict) or any(name not in record for name in DATABASE_COLUMNS):
			raise ValueError("skill snapshot has missing _RefSkill columns")
		row = []
		for name in DATABASE_COLUMNS:
			value = record[name]
			if isinstance(value, bool) or not isinstance(value, (str, int)):
				raise ValueError(f"invalid _RefSkill value in {name}")
			cell = str(value)
			if any(char in cell for char in "\t\r\n\0"):
				raise ValueError(f"invalid skill cell delimiter in {name}")
			row.append(cell)
		rows.append(row)
	skills.index_skills(rows)
	return rows


# ================
# load_database_rows
# ================
def load_database_rows(path):
	raw = Path(path).read_bytes()
	rows = project_database_rows(json.loads(raw.decode("utf-8-sig")))
	if not any(r[skills.SERVICE] == "1" and skills.player_skill(r[skills.CODENAME])
		and 100 < int(r[skills.MASTERY_LEVEL]) <= MAX_MASTERY for r in rows):
		raise ValueError("database snapshot contains no enabled mastery 101-110 player skills")
	return rows, sro_pk2.digest(raw)


# ================
# merge_database_tier
#
# Client tier first, database tier second: published client IDs/groups become
# immutable native inputs to the second pass. Cross-source numeric IDs never
# become runtime identities, and existing marker entries survive rebuilds.
# ================
def merge_database_tier(native, client, database, prior):
	client_codes = {r[skills.CODENAME] for r in client}
	old_ids = prior.get("idMap", {})
	client_ids = {code: entry for code, entry in old_ids.items() if code in client_codes}
	added, summary = skills.merge_skills(native, client, previous_ids=client_ids,
		previous_groups=prior.get("newGroupIds"))
	if not database:
		if len(client_ids) != len(old_ids):
			raise ValueError("published database skills require the configured database snapshot")
		return added, summary
	# Restrict new content to the approved tier; lower rows remain reference
	# anchors and later content must never expand the cap accidentally.
	database = [r for r in database if int(r[skills.MASTERY_LEVEL]) <= MAX_MASTERY]
	database_ids = {code: entry for code, entry in old_ids.items() if code not in client_codes}
	extra, report = skills.merge_skills(native + added, database, previous_ids=database_ids,
		previous_groups=prior.get("databaseNewGroupIds"))
	if any(int(r[skills.MASTERY_LEVEL]) <= 100 for r in extra):
		raise ValueError("database supplement must contain only mastery 101-110 additions")
	for key in ("added", "learnableRanks", "chainSubRanks", "groupsMappedToNative"):
		summary[key] += report[key]
	for key in ("masteries", "races"):
		for value, count in report[key].items():
			summary[key][value] = summary[key].get(value, 0) + count
	summary["idMap"].update(report["idMap"])
	summary["databaseNewGroupIds"] = report["newGroupIds"]
	summary["databaseTier"] = {key: report[key] for key in ("added", "learnableRanks", "chainSubRanks")}
	return added + extra, summary
