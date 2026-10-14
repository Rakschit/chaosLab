package institution

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func pemOf(t *testing.T, pub any) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func claims(now time.Time, mod func(m map[string]any)) map[string]any {
	m := map[string]any{
		"iss": "sicsr", "aud": "cyber-sim", "sub": "stu-1001", "name": "Asha",
		"state": "abc", "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	}
	if mod != nil {
		mod(m)
	}
	return m
}

func signRS(t *testing.T, key *rsa.PrivateKey, alg string, c map[string]any) string {
	h, _ := json.Marshal(map[string]string{"alg": alg, "typ": "JWT"})
	p, _ := json.Marshal(c)
	in := b64(h) + "." + b64(p)
	d := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, d[:])
	if err != nil {
		t.Fatal(err)
	}
	return in + "." + b64(sig)
}

func signES(t *testing.T, key *ecdsa.PrivateKey, c map[string]any) string {
	h, _ := json.Marshal(map[string]string{"alg": "ES256", "typ": "JWT"})
	p, _ := json.Marshal(c)
	in := b64(h) + "." + b64(p)
	d := sha256.Sum256([]byte(in))
	r, s, err := ecdsa.Sign(rand.Reader, key, d[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return in + "." + b64(sig)
}

func TestVerify(t *testing.T) {
	now := time.Now()
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	ek, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rsaPEM, ecPEM := pemOf(t, &rk.PublicKey), pemOf(t, &ek.PublicKey)

	ok := func(name, tok, key string) {
		t.Helper()
		c, err := Verify(tok, key, "sicsr", "cyber-sim", "abc", now)
		if err != nil || c.Subject != "stu-1001" || c.Name != "Asha" {
			t.Errorf("%s: want success, got %v %+v", name, err, c)
		}
	}
	bad := func(name, tok, key string) {
		t.Helper()
		if _, err := Verify(tok, key, "sicsr", "cyber-sim", "abc", now); err == nil {
			t.Errorf("%s: want failure, got success", name)
		}
	}

	ok("rs256", signRS(t, rk, "RS256", claims(now, nil)), rsaPEM)
	ok("es256", signES(t, ek, claims(now, nil)), ecPEM)

	bad("wrong key", signRS(t, other, "RS256", claims(now, nil)), rsaPEM)
	bad("alg none", signRS(t, rk, "none", claims(now, nil)), rsaPEM)
	bad("alg HS256 with rsa key", signRS(t, rk, "HS256", claims(now, nil)), rsaPEM)
	bad("es token with rsa key", signES(t, ek, claims(now, nil)), rsaPEM)
	bad("wrong issuer", signRS(t, rk, "RS256", claims(now, func(m map[string]any) { m["iss"] = "x" })), rsaPEM)
	bad("wrong audience", signRS(t, rk, "RS256", claims(now, func(m map[string]any) { m["aud"] = "x" })), rsaPEM)
	bad("wrong state", signRS(t, rk, "RS256", claims(now, func(m map[string]any) { m["state"] = "zzz" })), rsaPEM)
	bad("empty state", signRS(t, rk, "RS256", claims(now, func(m map[string]any) { m["state"] = "" })), rsaPEM)
	bad("expired", signRS(t, rk, "RS256", claims(now, func(m map[string]any) {
		m["iat"] = now.Add(-30 * time.Minute).Unix()
		m["exp"] = now.Add(-20 * time.Minute).Unix()
	})), rsaPEM)
	bad("too long lived", signRS(t, rk, "RS256", claims(now, func(m map[string]any) {
		m["exp"] = now.Add(2 * time.Hour).Unix()
	})), rsaPEM)
	bad("iat in future", signRS(t, rk, "RS256", claims(now, func(m map[string]any) {
		m["iat"] = now.Add(10 * time.Minute).Unix()
		m["exp"] = now.Add(12 * time.Minute).Unix()
	})), rsaPEM)
	bad("bad subject", signRS(t, rk, "RS256", claims(now, func(m map[string]any) { m["sub"] = "a b;c" })), rsaPEM)
	bad("no subject", signRS(t, rk, "RS256", claims(now, func(m map[string]any) { delete(m, "sub") })), rsaPEM)
	bad("garbage", "a.b.c", rsaPEM)
	bad("empty", "", rsaPEM)
	bad("no key", signRS(t, rk, "RS256", claims(now, nil)), "")

	// A tampered payload must fail even with a valid-looking signature.
	good := signRS(t, rk, "RS256", claims(now, nil))
	parts := splitDot(good)
	forged, _ := json.Marshal(claims(now, func(m map[string]any) { m["sub"] = "admin" }))
	bad("tampered payload", parts[0]+"."+b64(forged)+"."+parts[2], rsaPEM)
}

func splitDot(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func TestParsePublicKeyRejectsSmallRSA(t *testing.T) {
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	if _, err := ParsePublicKey(pemOf(t, &small.PublicKey)); err == nil {
		t.Error("1024-bit RSA key should be rejected")
	}
}
