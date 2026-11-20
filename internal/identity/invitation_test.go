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
	link, e := SignInvitation(cert.PrivateKey.(ed25519.PrivateKey), "127.0.0.1:7443", "client", make([]byte, 32), expires)
	if e != nil {
		t.Fatal(e)
	}
	i, e := ParseInvitation(link)
	if e != nil || i.Pin != Pin(cert.Leaf) || !i.Expires.Equal(expires) || i.Role != "client" || i.Address != "127.0.0.1:7443" {
		t.Fatal("incorrect invitation")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(link, InvitePrefix))
	for idx := range raw {
		raw[idx] ^= 1
		if _, e := ParseInvitation(InvitePrefix + base64.RawURLEncoding.EncodeToString(raw)); e == nil {
			t.Fatalf("accepted modified byte %d", idx)
		}
		raw[idx] ^= 1
	}
	old, e := ParseInvitation("clistream://pair#address=localhost%3A7443&pin=" + i.Pin + "&token=" + i.ID)
	if e != nil || old.ID != i.ID || old.Pin != i.Pin {
		t.Fatal("legacy link failed")
	}
}
