/*
===========================================================================

referrals_test.go - the referral routes: served only with the flag, the
invited accounts never named, the refusals mapped

===========================================================================
*/
package publicstats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
fakeReferrals

An in-memory ReferralSource: alpha owns code ALPHA234.
================
*/
type fakeReferrals struct {
	rows     []domain.Referral
	approved []string
}

func (f *fakeReferrals) ReferralCode(account string) (string, error) {
	return strings.ToUpper(account) + "234", nil
}

func (f *fakeReferrals) ReferralCodeOwner(code string) (string, error) {
	if code == "ALPHA234" {
		return "alpha", nil
	}
	return "", domain.ErrReferralUnknownCode
}

func (f *fakeReferrals) LinkReferral(referee, code string, held bool) error {
	owner, err := f.ReferralCodeOwner(code)
	if err != nil {
		return err
	}
	if owner == referee {
		return domain.ErrReferralSelf
	}
	for _, row := range f.rows {
		if row.Referee == referee {
			return domain.ErrReferralLinked
		}
	}
	f.rows = append(f.rows, domain.Referral{Referee: referee, Referrer: owner, LinkedMs: 1, Held: held})
	return nil
}

func (f *fakeReferrals) ReferralsBy(referrer string) ([]domain.Referral, error) {
	out := []domain.Referral{}
	for _, row := range f.rows {
		if row.Referrer == referrer {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeReferrals) HeldReferrals(int) ([]domain.Referral, error) {
	out := []domain.Referral{}
	for _, row := range f.rows {
		if row.Held {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeReferrals) ApproveReferral(referee string) error {
	for i := range f.rows {
		if f.rows[i].Referee == referee {
			f.rows[i].Held = false
			f.approved = append(f.approved, referee)
			return nil
		}
	}
	return domain.ErrReferralNotFound
}

/*
================
authorizedPut
================
*/
func authorizedPut(s *Service, path, body string) int {
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testWriteToken)
	s.Handler().ServeHTTP(rec, request)
	return rec.Code
}

/*
================
TestReferralRoutesAreUnservedWithTheFlagOff
================
*/
func TestReferralRoutesAreUnservedWithTheFlagOff(t *testing.T) {
	s := privacyService(newFixture(), testWriteToken)
	for _, path := range []string{"/v1/accounts/alpha/referrals", "/v1/referral-codes/ALPHA234", "/v1/referrals/held"} {
		if code, _ := authorizedGet(s, path, testWriteToken); code != http.StatusNotFound {
			t.Errorf("%s answered %d with SRO_REFERRALS off, want 404", path, code)
		}
	}
	if code := authorizedPut(s, "/v1/accounts/gamma/referrer", `{"code":"ALPHA234"}`); code != http.StatusNotFound {
		t.Errorf("link answered %d with SRO_REFERRALS off, want 404", code)
	}
}

/*
================
TestReferralRoutesLinkListAndApprove
================
*/
func TestReferralRoutesLinkListAndApprove(t *testing.T) {
	f := newFixture()
	f.chars[2].AccountID = "alpha"
	s := privacyService(f, testWriteToken)
	referrals := &fakeReferrals{}
	s.src.Referrals = referrals
	s.src.ReferralRules = ReferralRules{Level: 30, ReferrerSilk: 100, RefereeSilk: 50, RewardCap: 10}

	if code, _ := authorizedGet(s, "/v1/accounts/alpha/referrals", ""); code != http.StatusUnauthorized {
		t.Fatalf("no token answered %d, want 401", code)
	}
	if code := authorizedPut(s, "/v1/accounts/other/referrer", `{"code":"NOPE2345"}`); code != http.StatusNotFound {
		t.Fatalf("unknown code link answered %d, want 404", code)
	}
	if code := authorizedPut(s, "/v1/accounts/other/referrer", `{"code":"ALPHA234","extra":1}`); code != http.StatusBadRequest {
		t.Fatalf("unknown field answered %d, want 400", code)
	}
	if code := authorizedPut(s, "/v1/accounts/gamma/referrer", `{"code":"ALPHA234"}`); code != http.StatusNoContent {
		t.Fatalf("link answered %d", code)
	}
	if code := authorizedPut(s, "/v1/accounts/gamma/referrer", `{"code":"ALPHA234"}`); code != http.StatusConflict {
		t.Fatalf("second link answered %d, want 409", code)
	}
	if code := authorizedPut(s, "/v1/accounts/alpha/referrer", `{"code":"ALPHA234"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("self link answered %d, want 422", code)
	}
	if code := authorizedPut(s, "/v1/accounts/delta/referrer", `{"code":"ALPHA234","held":true}`); code != http.StatusNoContent {
		t.Fatalf("held link answered %d", code)
	}

	code, body := authorizedGet(s, "/v1/accounts/alpha/referrals", testWriteToken)
	if code != http.StatusOK {
		t.Fatalf("own referrals answered %d", code)
	}
	if strings.Contains(string(body), "gamma") || strings.Contains(string(body), "delta") {
		t.Fatalf("own referrals name an invited account: %s", body)
	}
	var own OwnReferralsResponse
	if err := json.Unmarshal(body, &own); err != nil {
		t.Fatal(err)
	}
	if own.Code != "ALPHA234" || own.Rules.Level != 30 || len(own.Invites) != 2 ||
		own.Invites[0].Status != referralJoined || own.Invites[1].Status != referralHeld {
		t.Fatalf("own referrals %+v", own)
	}

	code, body = authorizedGet(s, "/v1/referral-codes/ALPHA234", testWriteToken)
	if code != http.StatusOK || !strings.Contains(string(body), `"inviter":"Kekw"`) || !strings.Contains(string(body), `"account":"alpha"`) {
		t.Fatalf("code lookup %d %s, want the shown character Kekw", code, body)
	}
	if code, _ := authorizedGet(s, "/v1/referral-codes/NOPE2345", testWriteToken); code != http.StatusNotFound {
		t.Fatalf("unknown code answered %d", code)
	}

	code, body = authorizedGet(s, "/v1/referrals/held", testWriteToken)
	if code != http.StatusOK || !strings.Contains(string(body), `"referee":"delta"`) || strings.Contains(string(body), "gamma") {
		t.Fatalf("held list %d %s", code, body)
	}
	if code := authorizedPut(s, "/v1/referrals/delta/approved", ""); code != http.StatusNoContent {
		t.Fatalf("approve answered %d", code)
	}
	if code := authorizedPut(s, "/v1/referrals/nobody/approved", ""); code != http.StatusNotFound {
		t.Fatalf("approve unknown answered %d", code)
	}
}
