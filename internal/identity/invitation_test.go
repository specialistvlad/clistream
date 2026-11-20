package identity

import (
	"crypto/ed25519"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSignedInvitationIntegrity(t *testing.T) {
	cert, e := Key(filepath.Join(t.TempDir(), "key"))
	if e != nil {
		t.Fatal(e)
	}
	expires := time.Unix(1000, 0)
	link, e := SignInvitation(cert.PrivateKey.(ed25519.PrivateKey), "127.0.0.1:7443", make([]byte, 32), expires)
	if e != nil {
		t.Fatal(e)
	}
	i, e := ParseInvitation(link)
	if e != nil || i.Pin != Pin(cert.Leaf) || !i.Expires.Equal(expires) || i.Version != 2 || i.Role != "" || i.Address != "127.0.0.1:7443" {
		t.Fatal("incorrect invitation")
	}
	const prefix = "clistream://127.0.0.1/?token="
	token, ok := strings.CutPrefix(link, prefix)
	if !ok {
		t.Fatal("unexpected invitation URL")
	}
	oldV2, err := ParseInvitation(legacyV2Prefix + token)
	if err != nil || oldV2 != i {
		t.Fatal("legacy v2 link failed")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	for idx := range raw {
		raw[idx] ^= 1
		if _, e := ParseInvitation(prefix + base64.RawURLEncoding.EncodeToString(raw)); e == nil {
			t.Fatalf("accepted modified byte %d", idx)
		}
		raw[idx] ^= 1
	}
	old, e := ParseInvitation("clistream://pair#address=localhost%3A7443&pin=" + i.Pin + "&token=" + i.ID)
	if e != nil || old.ID != i.ID || old.Pin != i.Pin {
		t.Fatal("legacy link failed")
	}
}

func TestInvitationURL(t *testing.T) {
	cert, err := Key(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ address, host string }{{"broker.example:7443", "broker.example"}, {"broker.example:9443", "broker.example"}, {"[::1]:9123", "[::1]"}, {"[fe80::1%en0]:7443", "[fe80::1%25en0]"}} {
		link, err := SignInvitation(cert.PrivateKey.(ed25519.PrivateKey), tc.address, make([]byte, idSize), time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		prefix := "clistream://" + tc.host + "/?token="
		token, ok := strings.CutPrefix(link, prefix)
		if !ok || token == "" {
			t.Fatal("incorrect advertised hostname")
		}
		invite, err := ParseInvitation(link)
		if err != nil || invite.Address != tc.address {
			t.Fatal("signed endpoint did not round trip")
		}
		for _, modified := range []string{"clistream://elsewhere/?token=" + token, "clistream://user@" + tc.host + "/?token=" + token, link + "&token=" + token, link + "#fragment", prefix, link + "\n"} {
			if _, err := ParseInvitation(modified); err == nil {
				t.Fatal("accepted malformed or mismatched invitation")
			}
		}
	}
}
