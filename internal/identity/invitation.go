package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const InvitePrefix = "clistream://pair/v1/"
const signatureDomain = "clistream/pair/v1\x00"
const (
	idSize         = 32
	expiryOffset   = ed25519.PublicKeySize
	idOffset       = expiryOffset + 8
	roleOffset     = idOffset + idSize
	endpointOffset = roleOffset + 1
)

type Invitation struct {
	Profile
	ID      string
	Role    string
	Expires time.Time
}

func validEndpoint(s string) bool {
	host, port, err := net.SplitHostPort(s)
	n, e := strconv.ParseUint(port, 10, 16)
	if err != nil || e != nil || n == 0 || len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, c := range host {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(".-:_%", c)) {
			return false
		}
	}
	return true
}

// The signed bytes are public key, Unix expiry, random ID, role byte and endpoint.
func SignInvitation(key ed25519.PrivateKey, address, role string, id []byte, expires time.Time) (string, error) {
	if !validEndpoint(address) || len(id) != idSize || (role != "client" && role != "supervisor") {
		return "", errors.New("invalid invitation parameters")
	}
	payload := append([]byte(nil), key.Public().(ed25519.PublicKey)...)
	payload = binary.BigEndian.AppendUint64(payload, uint64(expires.Unix()))
	payload = append(payload, id...)
	payload = append(payload, role[0])
	payload = append(payload, address...)
	sig := ed25519.Sign(key, append([]byte(signatureDomain), payload...))
	return InvitePrefix + base64.RawURLEncoding.EncodeToString(append(payload, sig...)), nil
}

func ParseInvitation(link string) (Invitation, error) {
	invalid := errors.New("invalid invitation")
	if !strings.HasPrefix(link, InvitePrefix) {
		// Old daemons issue an unsigned URL with a one-use token and TLS pin.
		u, e := url.Parse(link)
		if e != nil || u.Scheme != "clistream" || u.Host != "pair" || u.Path != "" || u.User != nil || u.RawQuery != "" {
			return Invitation{}, invalid
		}
		q, e := url.ParseQuery(u.Fragment)
		id, err := hex.DecodeString(q.Get("token"))
		pin, pe := hex.DecodeString(q.Get("pin"))
		if e != nil || err != nil || pe != nil || len(id) != 32 || len(pin) != 32 || !validEndpoint(q.Get("address")) {
			return Invitation{}, invalid
		}
		return Invitation{Profile: Profile{q.Get("address"), q.Get("pin")}, ID: q.Get("token")}, nil
	}
	encoded := strings.TrimPrefix(link, InvitePrefix)
	if len(encoded) > 2048 {
		return Invitation{}, invalid
	}
	raw, e := base64.RawURLEncoding.Strict().DecodeString(encoded)

	if e != nil || len(raw) <= endpointOffset+ed25519.SignatureSize {
		return Invitation{}, invalid
	}
	payload, sig := raw[:len(raw)-ed25519.SignatureSize], raw[len(raw)-ed25519.SignatureSize:]
	key := ed25519.PublicKey(payload[:expiryOffset])
	if !ed25519.Verify(key, append([]byte(signatureDomain), payload...), sig) {
		return Invitation{}, invalid
	}
	address := string(payload[endpointOffset:])
	role := ""
	switch payload[roleOffset] {
	case 'c':
		role = "client"
	case 's':
		role = "supervisor"
	}
	if !validEndpoint(address) || role == "" {
		return Invitation{}, invalid
	}
	der, e := x509.MarshalPKIXPublicKey(key)
	if e != nil {
		return Invitation{}, e
	}
	pin := sha256.Sum256(der)
	return Invitation{
		Profile: Profile{address, hex.EncodeToString(pin[:])},
		ID:      hex.EncodeToString(payload[idOffset:roleOffset]), Role: role,
		Expires: time.Unix(int64(binary.BigEndian.Uint64(payload[expiryOffset:idOffset])), 0),
	}, nil
}
