/*
===========================================================================

notice_test.go - notices cannot become player credentials or survive expiry

===========================================================================
*/
package auth

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
================
TestSignedNoticeBoundaries
================
*/
func TestSignedNoticeBoundaries(t *testing.T) {
	now := time.Unix(1800000000, 0)
	private, err := GenerateAgentSessionKeyRing(now)
	if err != nil {
		t.Fatal(err)
	}
	public, err := PublicAgentSessionKeyRing(private)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	privatePath, publicPath := filepath.Join(directory, "private.json"), filepath.Join(directory, "public.json")
	if err := os.WriteFile(privatePath, private, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, public, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := NewAgentSessionSigner(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewAgentSessionVerifier(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.MintNotice("global-official", "Maintenance in 5 minutes.", now)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.VerifyNotice(token, now)
	if err != nil || claims.ShardID != "global-official" || claims.Message != "Maintenance in 5 minutes." {
		t.Fatalf("notice = %+v, %v", claims, err)
	}
	if _, err := verifier.Verify(token, now); err == nil {
		t.Fatal("notice accepted as player session")
	}
	session, err := signer.Mint("player", "global-official", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimPrefix(token, noticeDomain), ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	tampered := noticeDomain + base64.RawURLEncoding.EncodeToString([]byte(strings.ReplaceAll(string(payload), "Maintenance", "Unauthorized"))) + "." + parts[1]
	for name, value := range map[string]string{"tampered": tampered, "session": session, "truncated": token[:len(token)-4], "oversized": strings.Repeat("x", noticeMaxTokenBytes+1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier.VerifyNotice(value, now); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
	for _, at := range []time.Time{now.Add(NoticeLifetime), now.Add(-time.Second)} {
		if _, err := verifier.VerifyNotice(token, at); err == nil {
			t.Fatal("notice outside its lifetime accepted")
		}
	}
	for _, message := range []string{"", "\nhello", strings.Repeat("a", 101), strings.Repeat("\U0001F600", 51), string([]byte{0xff})} {
		if _, err := signer.MintNotice("global-official", message, now); err == nil {
			t.Fatalf("invalid text accepted: %q", message)
		}
	}
	if _, err := signer.MintNotice("global-official", strings.Repeat("\U0001F600", 50), now); err != nil {
		t.Fatal(err)
	}
}
