# Account registration

The login page's **Register** button opens **Username**, **Password** and
**Confirm password**. The username becomes the login ID and is trimmed before
creation; its spelling is preserved and login ignores ASCII case. Usernames
must fit the existing 64-byte account ID limit. Passwords need at least eight
characters and at most 72 UTF-8 bytes. Both password fields must match before
submission. These requirements appear below the fields. Passwords use the
account store's existing bcrypt policy.

After registration the player returns to login and signs in with that username
and password. No email is sent and no confirmation is required.

This is port-only functionality, enabled by default at the owner's request.
Set `SRO_REGISTRATION=0` on the Agent to disable public signup. The client
Experimental window's **Account registration** option hides the button when
disabled; the server setting controls whether accounts can actually be created.

`SRO_REGISTRATION_REQUIRE_EMAIL_CONFIRMATION=1` reserves confirmation as an
operator option. Until mail delivery and confirmation tokens are implemented,
this setting refuses signup before creating an account. Its default is off.
It does not silently accept unconfirmed accounts or send placeholder emails.

`POST /title/register` accepts `{ "username": "PlayerOne", "password": "..." }`
through the same release protocol, origin checks, readiness gate and public
Agent routing as login. Success returns HTTP 201 with `ok: true` and
`emailConfirmationRequired: false`. Duplicate usernames return HTTP 409. The
endpoint shares the login rate limit and bcrypt concurrency budget. The
private provisioning API remains a separate authenticated listener.
