/*
===========================================================================

opcodes.ts - the wire opcodes the session worker names

One table for the opcodes the client compares, sends and journals, so a
handler reads OBJECT_DESPAWN_OPCODE instead of 0x36AB. Each name follows
the server's constant for the same value (apps/server/internal, the
item/wire, social and simulation packages); a value the server has not
named yet stays a literal at its use until it is named here.

Two names cover one value on purpose elsewhere: 0x3012 is GAME_READY_OPCODE here
(the empty world-load signal) and FRIEND_CONSENT_OPCODE in
foundation/gameplay/friends.ts (the 9-byte prompt answer on the same
opcode).

===========================================================================
*/

// 0x3xxx: one-way notices (mostly server pushes; 0x3012 rides client to server)
export const GAME_READY_OPCODE = 0x3012;
export const EVENT_NOTICE_OPCODE = 0x3058;
export const FLAG_WAR_EVENT_OPCODE = 0x30a9;
export const POINTS_UPDATE_OPCODE = 0x30b3;
export const GACHA_WIN_ANNOUNCE_OPCODE = 0x30c0;
export const OBJECT_LIST_START_OPCODE = 0x30cb;
export const SINGLE_OBJECT_SPAWN_OPCODE = 0x30d7;
export const STALL_OPENED_OPCODE = 0x30df;
export const OBJECT_SOURCE_MOVE_OPCODE = 0x30e3;
export const PARTY_JOIN_ANSWER_OPCODE = 0x30fa;
export const OBJECT_STATE_REFRESH_OPCODE = 0x3122;
export const COS_RECORD_CREATE_OPCODE = 0x3158;
export const GAME_TIME_OPCODE = 0x31ad;
export const GROUND_OWNERSHIP_EXPIRED_OPCODE = 0x31e2;
export const SERVER_CLOCK_GID_LATCH_OPCODE = 0x32a6;
export const GUILD_WAR_SEED_OPCODE = 0x32bb;
export const LOCAL_REBIRTH_REQUEST_OPCODE = 0x32dc;
export const OBJECT_LIST_FINALIZE_OPCODE = 0x330a;
export const RESET_CLIENT_OPCODE = 0x3369;
export const VITALS_UPDATE_OPCODE = 0x33a6;
export const STALL_CLOSED_OPCODE = 0x33d1;
export const OBJECT_LIST_CHUNK_OPCODE = 0x3417;
export const JOB_DRESS_BAR_OPCODE = 0x3434;
export const BASE_STATS_OPCODE = 0x343c;
export const STALL_RENAMED_OPCODE = 0x34b7;
export const COS_INFO_UPDATE_OPCODE = 0x3508;
export const JOB_DRESS_CANCEL_OPCODE = 0x3514;
export const PICKUP_ANIM_OPCODE = 0x35c7;
export const PARTY_INFO_OPCODE = 0x35d6;
export const CHAT_BROADCAST_OPCODE = 0x3667;
export const COS_STATE_REFRESH_OPCODE = 0x3691;
export const OBJECT_DESPAWN_OPCODE = 0x36ab;
export const LEVEL_UP_EFFECT_OPCODE = 0x36b0;
export const QUEST_GATHER_START_OPCODE = 0x36bd;
export const QUEST_NOTIFICATION_OPCODE = 0x36bf;
export const ABNORMAL_SNAPSHOT_OPCODE = 0x36c7;
export const MOVEMENT_SPEED_OPCODE = 0x376f;
export const FORTRESS_WAR_STATE_OPCODE = 0x3887;
export const GUILD_UPDATE_PUSH_OPCODE = 0x3b29;
export const PARTY_UPDATE_OPCODE = 0x3e58;

// 0x7xxx: client requests
export const MOTION_STATE_REQUEST_OPCODE = 0x7017;
export const ITEM_MOVE_REQUEST_OPCODE = 0x706d;
export const TC_GRADUATE_REQUEST_OPCODE = 0x710a;
export const REBIRTH_POINT_APPOINT_REQUEST_OPCODE = 0x720d;
export const TARGET_INTERACT_OPCODE = 0x72cd;
export const TC_HONOR_RANK_REQUEST_OPCODE = 0x72d0;
export const NPC_ACTION_REQUEST_OPCODE = 0x7338;
export const CHAT_REQUEST_OPCODE = 0x7367;
export const TC_COMPULSORY_GRADUATE_REQUEST_OPCODE = 0x736d;
export const MERCENARY_DISMISS_OPCODE = 0x7458;
export const GUILD_WAR_SURRENDER_OPCODE = 0x7465;
export const COS_RIDE_REQUEST_OPCODE = 0x74b5;
export const TC_KICK_REQUEST_OPCODE = 0x74d4;
export const COS_CANCEL_REQUEST_OPCODE = 0x756c;
export const ITEM_USE_REQUEST_OPCODE = 0x75bd;
export const COS_TERMINATE_REQUEST_OPCODE = 0x7618;
export const WHISPER_BLOCK_REQUEST_OPCODE = 0x766f;
export const VISUAL_FLAGS_REQUEST_OPCODE = 0x7683;
export const COS_COMMAND_REQUEST_OPCODE = 0x769e;
export const GUILD_WAR_DECLARE_OPCODE = 0x771b;
export const FLAG_WAR_REGISTER_REQUEST_OPCODE = 0x7753;
export const QUEST_GATHER_CANCEL_OPCODE = 0x775d;
export const TC_LEAVE_REQUEST_OPCODE = 0x7785;
export const TC_DISBAND_REQUEST_OPCODE = 0x77c5;

// 0xBxxx: server answers to those requests
export const GACHA_RESULT_OPCODE = 0xb053;
export const ITEM_MOVE_RESPONSE_OPCODE = 0xb06d;
export const MASTERY_LEVEL_UP_RESPONSE_OPCODE = 0xb165;
export const STALL_EDIT_RESULT_OPCODE = 0xb1a8;
export const EXCHANGE_RESULT_OPCODE = 0xb1be;
export const INTERACTION_RESPONSE_OPCODE = 0xb1e1;
export const REBIRTH_POINT_APPOINT_RESULT_OPCODE = 0xb20d;
export const SKILL_CAST_RESULT_OPCODE = 0xb245;
export const ALLOC_STR_RESPONSE_OPCODE = 0xb27a;
export const SKILL_LEARN_RESPONSE_OPCODE = 0xb2cb;
export const OBJECT_STEER_OPCODE = 0xb2cf;
export const TC_HONOR_RANK_ANSWER_OPCODE = 0xb2d0;
export const OBJECT_SOURCE_CORRECTION_OPCODE = 0xb2f5;
export const NPC_INTERACTION_ACK_OPCODE = 0xb338;
export const CHAT_ACK_OPCODE = 0xb367;
export const PARTY_MODIFY_ACK_OPCODE = 0xb3dc;
export const OBJECT_SELECT_RESULT_OPCODE = 0xb45a;
export const COS_RIDE_STATE_OPCODE = 0xb4b5;
export const PARTY_DELETE_ACK_OPCODE = 0xb535;
export const ALLOC_INT_RESPONSE_OPCODE = 0xb552;
export const COS_CANCEL_RESPONSE_OPCODE = 0xb56c;
export const MENTOR_JOIN_ACK_OPCODE = 0xb592;
export const GM_COMMAND_ACK_OPCODE = 0xb5b6;
export const ITEM_USE_RESPONSE_OPCODE = 0xb5bd;
export const PARTY_JOIN_ACK_OPCODE = 0xb5bf;
export const COS_TERMINATE_RESPONSE_OPCODE = 0xb618;
export const WHISPER_BLOCK_ACK_OPCODE = 0xb66f;
export const VISUAL_FLAGS_UPDATE_OPCODE = 0xb683;
export const COS_COMMAND_RESULT_OPCODE = 0xb69e;
export const PARTY_REGISTER_ACK_OPCODE = 0xb6ff;
export const MENTOR_LISTING_PAGE_OPCODE = 0xb701;
export const MOVEMENT_ACK_OPCODE = 0xb738;
export const QUEST_GATHER_CANCEL_REPLY_OPCODE = 0xb75d;
export const TC_DISBAND_ANSWER_OPCODE = 0xb7c5;
