/*
===========================================================================

exchange_test.go - the request book: an ask again cancels the first

===========================================================================
*/

package exchange

import "testing"

/*
================
TestDropOutgoingRequest
================
*/
func TestDropOutgoingRequest(t *testing.T) {
	r := New()
	r.Propose("div", "Ash", "Berk", 1000)
	r.Propose("div", "Cleo", "Dain", 1000)
	if !r.DropOutgoingRequest("div", "ash", 1001) {
		t.Fatal("the asker's live ask was not dropped")
	}
	if r.Pending("div", "Berk", 1001) {
		t.Fatal("the dropped ask still pends")
	}
	if !r.Pending("div", "Dain", 1001) {
		t.Fatal("another asker's ask was dropped")
	}
	if r.DropOutgoingRequest("div", "Ash", 1001) {
		t.Fatal("a second drop reports success with nothing out")
	}
	r.Propose("div", "Ash", "Berk", 1000)
	if r.DropOutgoingRequest("div", "Ash", 1000+AnswerWindowMs+1) {
		t.Fatal("an expired ask reports dropped")
	}
	if r.DropOutgoingRequest("other-division", "Cleo", 1001) {
		t.Fatal("an ask in another division was dropped")
	}
}
