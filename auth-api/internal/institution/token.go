// Package institution verifies the signed login tokens that institutions send
// back after authenticating one of their own students or faculty.
package institution

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// maxLifetime is the longest a token may be valid (exp - iat).
	maxLifetime = 15 * time.Minute
	// leeway tolerates small clock differences between the two servers.
	leeway = 60 * time.Second

	maxTokenBytes = 4096
	maxNameRunes  = 60
)

// ErrInvalidToken is returned for every verification failure. The reason is
// deliberately not exposed to callers, so the endpoint cannot be used to probe
// which check failed.
var ErrInvalidToken = errors.New("invalid institution token")

var participantIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:@+-]{1,128}$`)

// Claims are the verified contents of a token.
type Claims struct {
	Subject string // the institution's own ID for this person (opaque to us)
	Name    string // optional nickname, "" if none
}

type header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type rawClaims struct {
	Iss   string  `json:"iss"`
	Aud   string  `json:"aud"`
	Sub   string  `json:"sub"`
	Exp   float64 `json:"exp"`
	Iat   float64 `json:"iat"`
	State string  `json:"state"`
	Name  string  `json:"name"`
}

// ParsePublicKey reads a PEM public key (PKIX, "BEGIN PUBLIC KEY") holding an
// RSA key (2048 bits or more) or an ECDSA P-256 key.
func ParsePublicKey(pemText string) (crypto.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemText)))
	if block == nil {
		return nil, errors.New("public key is not valid PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.New("public key must be PKIX (BEGIN PUBLIC KEY)")
	}
	switch k := key.(type) {
	case *rsa.PublicKey:
		if k.N.BitLen() < 2048 {
			return nil, errors.New("RSA key must be at least 2048 bits")
		}
	case *ecdsa.PublicKey:
		if k.Curve.Params().Name != "P-256" {
			return nil, errors.New("ECDSA key must use curve P-256")
		}
	default:
		return nil, errors.New("unsupported public key type")
	}
	return key, nil
}

// Verify checks a compact JWT (RS256 or ES256) and returns its claims.
//
//   - the algorithm must match the type of the stored key, and "none" and
//     HMAC algorithms are never accepted;
//   - iss must equal issuer, aud must equal audience, and the state claim must
//     equal the state of this login attempt;
//   - the token must be unexpired and short-lived.
func Verify(token, pemKey, issuer, audience, state string, now time.Time) (Claims, error) {
	if len(token) == 0 || len(token) > maxTokenBytes {
		return Claims{}, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}

	var h header
	if err := decodePart(parts[0], &h); err != nil {
		return Claims{}, ErrInvalidToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}

	key, err := ParsePublicKey(pemKey)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	signed := []byte(parts[0] + "." + parts[1])
	digest := sha256.Sum256(signed)

	switch k := key.(type) {
	case *rsa.PublicKey:
		if h.Alg != "RS256" {
			return Claims{}, ErrInvalidToken
		}
		if rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], sig) != nil {
			return Claims{}, ErrInvalidToken
		}
	case *ecdsa.PublicKey:
		if h.Alg != "ES256" || len(sig) != 64 {
			return Claims{}, ErrInvalidToken
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(k, digest[:], r, s) {
			return Claims{}, ErrInvalidToken
		}
	default:
		return Claims{}, ErrInvalidToken
	}

	var c rawClaims
	if err := decodePart(parts[1], &c); err != nil {
		return Claims{}, ErrInvalidToken
	}

	if c.Iss != issuer || c.Aud != audience || c.State == "" || c.State != state {
		return Claims{}, ErrInvalidToken
	}
	if !participantIDPattern.MatchString(c.Sub) {
		return Claims{}, ErrInvalidToken
	}

	exp := time.Unix(int64(c.Exp), 0)
	iat := time.Unix(int64(c.Iat), 0)
	if c.Exp == 0 || c.Iat == 0 ||
		!now.Before(exp.Add(leeway)) ||
		iat.After(now.Add(leeway)) ||
		exp.Sub(iat) > maxLifetime {
		return Claims{}, ErrInvalidToken
	}

	return Claims{Subject: c.Sub, Name: cleanName(c.Name)}, nil
}

func decodePart(part string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// cleanName keeps an optional nickname only if it is short and printable.
func cleanName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > maxNameRunes {
		return ""
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return name
}
