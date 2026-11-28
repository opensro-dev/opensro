/*
===========================================================================

opcodes.ts - the wire opcodes the session worker names

One table for the opcodes the client compares, sends and journals, so a
handler reads OBJECT_DESPAWN_OPCODE instead of 0x36AB. A name follows the
server's constant for the same value (apps/server/internal); where the
server keeps none, it follows the native handler the client's decoder
cites (QUEST_ABORT_RESPONSE_OPCODE is CPSMission_OnQuestAbortResponse
0xB1EB) or what the decoder reads (WEATHER_OPCODE). 0x3FB2..0x3FBE are
the port's own pet-skill frames.

Two names cover one value on purpose elsewhere: 0x3012 is GAME_READY_OPCODE
here (the empty world-load signal) and FRIEND_CONSENT_OPCODE in
foundation/gameplay/friends.ts (the 9-byte prompt answer on the same
opcode). 0x7541 carries both quick-slot rows and the auto-potion settings
(payload byte 0 tells them apart). 0x7FCA stays ARENA_DECISION_OPCODE in
foundation/gameplay/arena-chest.ts.

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
export const QUEST_MARKER_REMOVE_OPCODE = 0x30ea;
export const PARTY_JOIN_ANSWER_OPCODE = 0x30fa;
export const OBJECT_STATE_REFRESH_OPCODE = 0x3122;
export const TARGET_ACTION_STATE_OPCODE = 0x314d;
export const COS_RECORD_CREATE_OPCODE = 0x3158;
export const PARTY_LOOT_NOTICE_OPCODE = 0x317d;
export const GAME_TIME_OPCODE = 0x31ad;
export const GROUND_OWNERSHIP_EXPIRED_OPCODE = 0x31e2;
export const QUEST_UPDATE_OPCODE = 0x31ed;
export const HOLD_PVP_STATE_OPCODE = 0x3204;
export const SKIN_CHANGE_OPCODE = 0x323a;
export const ACTION_EMOTE_OPCODE = 0x324b;
export const STALL_EVENT_OPCODE = 0x3260;
export const SERVER_CLOCK_GID_LATCH_OPCODE = 0x32a6;
export const GUILD_WAR_SEED_OPCODE = 0x32bb;
export const LOCAL_REBIRTH_REQUEST_OPCODE = 0x32dc;
export const OBJECT_LIST_FINALIZE_OPCODE = 0x330a;
export const EQUIP_VISUAL_OPCODE = 0x3314;
export const EVENT_GROUP_STATE_OPCODE = 0x3347;
export const ALCHEMY_STEP_RESULT_OPCODE = 0x3359;
export const RESET_CLIENT_OPCODE = 0x3369;
export const VITALS_UPDATE_OPCODE = 0x33a6;
export const STALL_CLOSED_OPCODE = 0x33d1;
export const OBJECT_LIST_CHUNK_OPCODE = 0x3417;
export const JOB_DRESS_BAR_OPCODE = 0x3434;
export const BASE_STATS_OPCODE = 0x343c;
export const ITEM_USE_VISUAL_OPCODE = 0x3449;
export const ACTION_SPEED_OPCODE = 0x3453;
export const QUEST_MARKER_OPCODE = 0x3498;
export const STALL_RENAMED_OPCODE = 0x34b7;
export const COS_INFO_UPDATE_OPCODE = 0x3508;
export const JOB_DRESS_CANCEL_OPCODE = 0x3514;
export const PICKUP_ANIM_OPCODE = 0x35c7;
export const PARTY_INFO_OPCODE = 0x35d6;
export const PEER_GUILD_APPEARANCE_OPCODE = 0x365f;
export const CHAT_BROADCAST_OPCODE = 0x3667;
export const RESET_REGION_OPCODE = 0x366a;
export const COS_STATE_REFRESH_OPCODE = 0x3691;
export const OBJECT_DESPAWN_OPCODE = 0x36ab;
export const LEVEL_UP_EFFECT_OPCODE = 0x36b0;
export const QUEST_GATHER_START_OPCODE = 0x36bd;
export const QUEST_NOTIFICATION_OPCODE = 0x36bf;
export const ABNORMAL_SNAPSHOT_OPCODE = 0x36c7;
export const RESET_CLIENT_ACK_OPCODE = 0x36dd;
export const GM_RESTRICTION_NOTICE_OPCODE = 0x36ea;
export const MOVEMENT_SPEED_OPCODE = 0x376f;
export const UNEQUIP_VISUAL_OPCODE = 0x377c;
export const PEER_GUILD_CLEARED_OPCODE = 0x37e6;
export const FORTRESS_WAR_STATE_OPCODE = 0x3887;
export const TC_STATUS_OPCODE = 0x3ac5;
export const GUILD_UPDATE_PUSH_OPCODE = 0x3b29;
export const WEATHER_OPCODE = 0x3bde;
export const TC_BUFF_STATE_OPCODE = 0x3cc1;
export const PARTY_UPDATE_OPCODE = 0x3e58;
export const PET_SHIELD_OPCODE = 0x3fb2;
export const PET_COUNTER_PULSE_OPCODE = 0x3fb3;
export const PET_BOND_OPCODE = 0x3fb4;
export const PET_SPIRIT_OPCODE = 0x3fb5;
export const TAILWIND_OPCODE = 0x3fb7;
export const GUIDING_FEATHERS_OPCODE = 0x3fb9;
export const CROW_VISUAL_OPCODE = 0x3fbb;
export const HUNTING_HOWL_OPCODE = 0x3fbd;
export const BROOD_WIND_OPCODE = 0x3fbe;

// 0x7xxx: client requests
export const TC_PASS_BUFF_REQUEST_OPCODE = 0x700c;
export const MOTION_STATE_REQUEST_OPCODE = 0x7017;
export const ITEM_MOVE_REQUEST_OPCODE = 0x706d;
export const TC_GRADUATE_REQUEST_OPCODE = 0x710a;
export const COMPOUND_REQUEST_OPCODE = 0x716f;
export const REBIRTH_POINT_APPOINT_REQUEST_OPCODE = 0x720d;
export const TC_NOTICE_EDIT_REQUEST_OPCODE = 0x7220;
export const TARGET_INTERACT_OPCODE = 0x72cd;
export const TC_HONOR_RANK_REQUEST_OPCODE = 0x72d0;
export const RETURN_CANCEL_REQUEST_OPCODE = 0x72dd;
export const TC_OPEN_REQUEST_OPCODE = 0x72ec;
export const NPC_ACTION_REQUEST_OPCODE = 0x7338;
export const BERSERK_REQUEST_OPCODE = 0x7341;
export const CHAT_REQUEST_OPCODE = 0x7367;
export const TC_COMPULSORY_GRADUATE_REQUEST_OPCODE = 0x736d;
export const MERCENARY_DISMISS_OPCODE = 0x7458;
export const GUILD_WAR_SURRENDER_OPCODE = 0x7465;
export const PORTAL_REQUEST_OPCODE = 0x7495;
export const COS_RIDE_REQUEST_OPCODE = 0x74b5;
export const TC_KICK_REQUEST_OPCODE = 0x74d4;
export const QUICK_SLOT_BINDING_OPCODE = 0x7541;
export const COS_CANCEL_REQUEST_OPCODE = 0x756c;
// The stable keeper buys the active transport back (6FF980 / 7782E0).
export const COS_SELL_REQUEST_OPCODE = 0x71dc;
export const ITEM_USE_REQUEST_OPCODE = 0x75bd;
export const COS_TERMINATE_REQUEST_OPCODE = 0x7618;
export const WHISPER_BLOCK_REQUEST_OPCODE = 0x766f;
export const VISUAL_FLAGS_REQUEST_OPCODE = 0x7683;
export const COS_COMMAND_REQUEST_OPCODE = 0x769e;
export const TC_INVITE_REQUEST_OPCODE = 0x76b1;
export const GUILD_WAR_DECLARE_OPCODE = 0x771b;
export const FLAG_WAR_REGISTER_REQUEST_OPCODE = 0x7753;
export const QUEST_GATHER_CANCEL_OPCODE = 0x775d;
export const TC_LEAVE_REQUEST_OPCODE = 0x7785;
export const TC_DISBAND_REQUEST_OPCODE = 0x77c5;
export const BUYBACK_REQUEST_OPCODE = 0x77e7;

// 0xBxxx: server answers to those requests
export const TC_BUFF_LIST_OPCODE = 0xb00c;
export const STALL_CREATE_RESULT_OPCODE = 0xb049;
export const GACHA_RESULT_OPCODE = 0xb053;
export const COS_BEHAVIOR_RESULT_OPCODE = 0xb05b;
export const ITEM_MOVE_RESPONSE_OPCODE = 0xb06d;
export const MENTOR_MODIFY_ACK_OPCODE = 0xb13e;
export const MASTERY_LEVEL_UP_RESPONSE_OPCODE = 0xb165;
export const COMPOUND_RESULT_OPCODE = 0xb16f;
export const STALL_EDIT_RESULT_OPCODE = 0xb1a8;
export const EXCHANGE_RESULT_OPCODE = 0xb1be;
export const INTERACTION_RESPONSE_OPCODE = 0xb1e1;
export const QUEST_ABORT_RESPONSE_OPCODE = 0xb1eb;
export const REBIRTH_POINT_APPOINT_RESULT_OPCODE = 0xb20d;
export const TC_NOTICE_EDIT_ACK_OPCODE = 0xb220;
export const SKILL_CAST_RESULT_OPCODE = 0xb245;
export const ALLOC_STR_RESPONSE_OPCODE = 0xb27a;
export const QUEST_ACCEPT_RESPONSE_OPCODE = 0xb29a;
export const STALL_NETWORK_BOUGHT_OPCODE = 0xb2ca;
export const SKILL_LEARN_RESPONSE_OPCODE = 0xb2cb;
export const ACTION_STATE_OPCODE = 0xb2cd;
export const OBJECT_STEER_OPCODE = 0xb2cf;
export const TC_HONOR_RANK_ANSWER_OPCODE = 0xb2d0;
export const RETURN_CANCEL_RESULT_OPCODE = 0xb2dd;
export const TC_OPEN_ANSWER_OPCODE = 0xb2ec;
export const OBJECT_SOURCE_CORRECTION_OPCODE = 0xb2f5;
export const NPC_INTERACTION_ACK_OPCODE = 0xb338;
export const BERSERK_RESPONSE_OPCODE = 0xb341;
export const CHAT_ACK_OPCODE = 0xb367;
export const REINFORCE_RESULT_OPCODE = 0xb373;
export const PARTY_MODIFY_ACK_OPCODE = 0xb3dc;
export const STALL_BUY_RESULT_OPCODE = 0xb3f9;
export const STALL_CLOSE_RESULT_OPCODE = 0xb42c;
export const OBJECT_SELECT_RESULT_OPCODE = 0xb45a;
export const PORTAL_RESULT_OPCODE = 0xb495;
export const COS_RIDE_STATE_OPCODE = 0xb4b5;
export const PARTY_DELETE_ACK_OPCODE = 0xb535;
export const DISSOLVE_RESULT_OPCODE = 0xb549;
export const ALLOC_INT_RESPONSE_OPCODE = 0xb552;
export const MENTOR_REGISTER_ACK_OPCODE = 0xb55d;
export const COS_CANCEL_RESPONSE_OPCODE = 0xb56c;
export const COS_SELL_RESPONSE_OPCODE = 0xb1dc;
export const MENTOR_JOIN_ACK_OPCODE = 0xb592;
export const GM_COMMAND_ACK_OPCODE = 0xb5b6;
export const ITEM_USE_RESPONSE_OPCODE = 0xb5bd;
export const PARTY_JOIN_ACK_OPCODE = 0xb5bf;
export const COS_TERMINATE_RESPONSE_OPCODE = 0xb618;
export const STALL_VISIT_RESULT_OPCODE = 0xb61f;
export const ALCHEMY_STONE_RESULT_OPCODE = 0xb651;
export const WHISPER_BLOCK_ACK_OPCODE = 0xb66f;
export const VISUAL_FLAGS_UPDATE_OPCODE = 0xb683;
export const COS_COMMAND_RESULT_OPCODE = 0xb69e;
export const ENDED_EFFECT_INSTANCES_OPCODE = 0xb6a0;
export const STALL_LEAVE_RESULT_OPCODE = 0xb6e7;
export const STALL_NETWORK_RESULT_OPCODE = 0xb6f9;
export const PARTY_REGISTER_ACK_OPCODE = 0xb6ff;
export const MENTOR_LISTING_PAGE_OPCODE = 0xb701;
export const MENTOR_DELETE_ACK_OPCODE = 0xb70b;
export const MOVEMENT_ACK_OPCODE = 0xb738;
export const QUEST_GATHER_CANCEL_REPLY_OPCODE = 0xb75d;
export const TC_DISBAND_ANSWER_OPCODE = 0xb7c5;
