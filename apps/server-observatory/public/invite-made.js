/*
===========================================================================
invite-made.js - what the Invite keys page says about keys it just made

A key linked to a Discord member is a grant: the website keeps the
allowance and the member collects a fresh code from the bot's Get my
beta key button, so the console is never shown one (members stay in
Discord; codes are not handed over by staff). A grant is always one key:
the website refuses any other count for a member. Plain keys are shown
once, here, with their copy buttons.
===========================================================================
*/

/*
================
madeStatus

The status line after a create: a grant or the keys to copy.
================
*/
export function madeStatus( result ) {
	if ( result.granted ) return "Granted: the member gets the code from the bot's Get my beta key button.";
	const count = result.keys.length;
	return `Made ${count} ${count === 1 ? "key" : "keys"}. Copy them now.`;
}

/*
================
createRequest

The create request from the form's values. A grant to a Discord member
is always one key whatever How many says (the website refuses any other
count for a member, which read as "cannot give a key" to an operator who
had typed a batch size first); without a member it is a plain batch.
================
*/
export function createRequest( form ) {
	const member = form.discordUserId.trim();
	return {
		count: member ? 1 : Number( form.count ),
		maxUses: Number( form.maxUses ),
		expiresInDays: Number( form.expiresInDays ),
		note: form.note.trim(),
		...(member ? { discordUserId: member, discordName: form.discordName.trim() } : {})
	};
}
