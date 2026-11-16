/*
===========================================================================

prompt_test.go - the Contributor login prompt and its persisted answers

The prompt asks once per entry until the account answers about its
member's current title holder; a move makes every other account of the
member answer again.

===========================================================================
*/
package contributor

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

/*
================
openTwoAccountMember

One member with two linked accounts, a1 the oldest, plus a non-member.
================
*/
func openTwoAccountMember(t *testing.T, dir string) *Registry {
	t.Helper()
	writeMembers(t, dir, `{"members":[{"member":"m1","accounts":["a1","a2"]}],"accounts":["a1"]}`, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPromptsAskEachContributorSessionOncePerEntry(t *testing.T) {
	r := openTwoAccountMember(t, t.TempDir())
	online := []Online{{SessionID: "s1", AccountID: "a1"}, {SessionID: "s2", AccountID: "a2"}, {SessionID: "s3", AccountID: "x9"}}
	want := []Prompt{{SessionID: "s1", Kind: PromptHolder}, {SessionID: "s2", Kind: PromptElsewhere}}
	if got := r.Prompts(online); !reflect.DeepEqual(got, want) {
		t.Fatalf("first pass = %v, want %v", got, want)
	}
	if got := r.Prompts(online); len(got) != 0 {
		t.Fatalf("second pass asked again: %v", got)
	}
	// s2 leaves without answering; its next entry asks again.
	r.Prompts(online[:1])
	if got := r.Prompts(online); !reflect.DeepEqual(got, []Prompt{{SessionID: "s2", Kind: PromptElsewhere}}) {
		t.Fatalf("re-entry = %v", got)
	}
}

func TestKeepAnswersThePromptAndPersists(t *testing.T) {
	dir := t.TempDir()
	r := openTwoAccountMember(t, dir)
	if answer, _ := r.Command("a2", "/contributor keep"); answer != answerKept {
		t.Fatalf("keep elsewhere = %q", answer)
	}
	if answer, _ := r.Command("a1", "/contributor keep"); answer != answerShown {
		t.Fatalf("keep on the holder = %q", answer)
	}
	again, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
	if err != nil {
		t.Fatal(err)
	}
	online := []Online{{SessionID: "s1", AccountID: "a1"}, {SessionID: "s2", AccountID: "a2"}}
	if got := again.Prompts(online); len(got) != 0 {
		t.Fatalf("answered prompts came back after a reopen: %v", got)
	}
	if !again.Shown("a1") || again.Shown("a2") {
		t.Fatal("keep moved the title")
	}
}

func TestAMoveAsksTheOtherAccountsAgain(t *testing.T) {
	r := openTwoAccountMember(t, t.TempDir())
	r.Command("a1", "/contributor keep")
	r.Command("a2", "/contributor keep")
	if answer, _ := r.Command("a2", "/contributor here"); answer != answerMoved+" "+answerShown {
		t.Fatalf("here = %q", answer)
	}
	online := []Online{{SessionID: "s1", AccountID: "a1"}, {SessionID: "s2", AccountID: "a2"}}
	// a2 moved the title itself, so only a1 is asked, and now it lacks it.
	if got := r.Prompts(online); !reflect.DeepEqual(got, []Prompt{{SessionID: "s1", Kind: PromptElsewhere}}) {
		t.Fatalf("after the move = %v", got)
	}
}

func TestKeepFromANonMemberIsRefused(t *testing.T) {
	r := openTwoAccountMember(t, t.TempDir())
	if answer, handled := r.Command("x9", "/contributor keep"); !handled || answer != answerNotMember {
		t.Fatalf("non-member keep = %q, %v", answer, handled)
	}
}
