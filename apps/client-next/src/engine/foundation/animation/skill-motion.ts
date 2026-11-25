// v1.150 CCD620 state rows and BBDB70 authored set identities.
// Immutable scalar storage: callers cannot mutate a process-wide lookup table.
export const SKILL_MOTION_DEFAULT_SET_KEY = 0xCCCCC0;
const STATE_ROWS =
	"|ANI_ATTACK1=2|ANI_ATTACK2=5|ANI_ATTACK3=16|ANI_ATTACK4=17|ANI_ATTACK5=183|ANI_ATTACK6=184|ANI_ATTACK7=185|ANI_ATTACK8=186|ANI_ATTACK9=190|ANI_SKILL_1=26|ANI_SKILL_2=27|ANI_SKILL_3=28|ANI_SKILL_4=29|ANI_SKILL_5=30|ANI_SKILL_6=31|ANI_SKILL_7=32|ANI_SKILL_8=33|ANI_SKILL_9=34|ANI_SKILL_10=35|ANI_SKILL_11=68|ANI_SKILL_12=69|ANI_SKILL_13=70|ANI_SKILL_14=71|ANI_SKILL_15=72|ANI_SKILL_16=73|ANI_SKILL_17=74|ANI_SKILL_18=75|ANI_SKILL_19=76|ANI_SKILL_20=77|ANI_SKILL_21=101|ANI_SKILL_22=102|ANI_SKILL_23=103|ANI_SKILL_24=104|ANI_SKILL_25=105|ANI_SKILL_26=106|ANI_SKILL_27=107|ANI_SKILL_28=108|ANI_SKILL_29=109|ANI_SKILL_30=110|ANI_SKILL_31=111|ANI_SKILL_32=112|ANI_SKILL_33=113|ANI_SKILL_34=114|ANI_SKILL_35=115|ANI_SKILL_36=116|ANI_SKILL_37=117|ANI_SKILL_38=118|ANI_SKILL_39=119|ANI_SKILL_40=120|ANI_SKILL_41=123|ANI_SKILL_42=124|ANI_SKILL_43=125|ANI_SKILL_44=126|ANI_SKILL_45=127|ANI_SKILL_46=128|ANI_SKILL_47=129|ANI_SKILL_48=130|ANI_SKILL_49=131|ANI_SKILL_50=132|ANI_SKILL_51=133|ANI_SKILL_52=134|ANI_SKILL_53=135|ANI_SKILL_54=136|ANI_SKILL_55=137|ANI_SKILL_56=138|ANI_SKILL_57=139|ANI_SKILL_58=140|ANI_SKILL_59=141|ANI_SKILL_60=142|ANI_SKILL_61=143|ANI_SKILL_62=144|ANI_SKILL_63=145|ANI_SKILL_64=146|ANI_SKILL_65=147|ANI_SKILL_66=148|ANI_SKILL_67=149|ANI_SKILL_68=150|ANI_SKILL_69=151|ANI_SKILL_70=152|ANI_SKILL_71=153|ANI_SKILL_72=154|ANI_SKILL_73=155|ANI_SKILL_74=156|ANI_SKILL_75=157|ANI_SKILL_76=158|ANI_SKILL_77=159|ANI_SKILL_78=160|ANI_SKILL_79=161|ANI_SKILL_80=162|ANI_SKILL_81=163|ANI_SKILL_82=164|ANI_SKILL_83=165|ANI_SKILL_84=166|ANI_SKILL_85=167|ANI_SKILL_86=168|ANI_SKILL_87=169|ANI_SKILL_88=170|ANI_SKILL_89=171|ANI_SKILL_90=172|ANI_SKILL_91=173|ANI_SKILL_92=174|ANI_SKILL_93=175|ANI_SKILL_94=176|ANI_SKILL_95=177|ANI_SKILL_96=178|ANI_SKILL_97=179|ANI_SKILL_98=180|ANI_SKILL_99=181|ANI_SKILL_100=182|ANI_READY01=40|ANI_READY02=41|ANI_READY03=42|ANI_READY04=43|ANI_WAIT01=91|ANI_WAIT02=92|ANI_WAIT03=93|ANI_WAIT04=94|ANI_HAMMER=187|ANI_HANDLOOF=188|ANI_TROW=189|" +
	// Port-only, not native (SRO_NASRUN, #4407): the later clients' Devil's
	// Spirit shot state; the VSRO avatar_nasrun sets author it at 191.
	"ANI_SHOT=191|";
export function skillMotionStateName( id: number ): string | undefined {
	const end = STATE_ROWS.indexOf( "=" + id + "|" );
	return end < 0 ? undefined : STATE_ROWS.slice( STATE_ROWS.lastIndexOf( "|", end ) + 1, end );
}
export function skillMotionRole( key: number, name: string ): string {
	if ( !/^ANI_[A-Z0-9_]+$/.test( name ) ) throw Error( "Unknown native skill animation state: " + name );
	const sets = [
		"default",
		"sword",
		"spear",
		"bow",
		"cart",
		"onehand_staff",
		"onehand_sword",
		"twohand_sword",
		"dagger",
		"dual_axe",
		"harf",
		"twohand_staff",
		"onehand_staff",
		// Port-only (SRO_NASRUN, #4407): animationSetNames.ts LATER_ANIMATION_SET_KEY_BY_NAME.
		"avatar_nasrun1",
		"avatar_nasrun2",
		"avatar_nasrun3"
	];
	const index = (key - SKILL_MOTION_DEFAULT_SET_KEY) / 28, set = sets[index];
	if ( !Number.isInteger( index ) || !set ) throw Error( "Unknown skill animation set" );
	const token = "|" + name + "=", start = STATE_ROWS.indexOf( token );
	if ( start < 0 ) throw Error( "Unknown native skill animation state: " + name );
	const at = start + token.length, id = Number( STATE_ROWS.slice( at, STATE_ROWS.indexOf( "|", at ) ) );
	return "native:" + set + ":" + id;
}
