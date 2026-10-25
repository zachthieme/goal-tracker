package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var purposeTestKey = []byte("0123456789abcdef0123456789abcdef")

// hmacOver is message's HMAC-SHA256 under purposeTestKey, worked out here
// rather than by the codec, so the tests pin what the codec signs.
func hmacOver(message string) string {
	mac := hmac.New(sha256.New, purposeTestKey)
	mac.Write([]byte(message))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// A session cookie verifies only when its signature is over the payload with
// "session:" in front of it: the flow cookie's signature over the same payload,
// or one over the bare payload as before, doesn't decode.
func TestASessionCookieVerifiesOnlyWithTheSessionPurpose(t *testing.T) {
	t.Parallel()

	c := sessionCodec{key: purposeTestKey}
	const payload = "7.2.1767225600.1767312000"
	if sess, ok := c.decode(payload + "." + hmacOver("session:"+payload)); !ok || sess.accountID != 7 || sess.generation != 2 {
		t.Errorf("a cookie signed for a session decodes as %+v, %v; want Account 7 at generation 2", sess, ok)
	}
	for name, sig := range map[string]string{
		"signed for the flow cookie": hmacOver("oidc-flow:" + payload),
		"signed without a purpose":   hmacOver(payload),
	} {
		if sess, ok := c.decode(payload + "." + sig); ok {
			t.Errorf("a session cookie %s decodes as %+v", name, sess)
		}
	}

	now := time.Unix(1767225600, 0)
	sess := session{accountID: 7, generation: 2, signedIn: now, lastSeen: now}
	if got, ok := c.decode(c.encode(sess)); !ok || got != sess {
		t.Errorf("an encoded session decodes as %+v, %v; want %+v", got, ok, sess)
	}
}

// A flow cookie verifies only when its signature is over the payload with
// "oidc-flow:" in front of it: a session's signature over the same payload
// doesn't.
func TestAFlowCookieVerifiesOnlyWithTheFlowPurpose(t *testing.T) {
	t.Parallel()

	s := &Server{sessions: sessionCodec{key: purposeTestKey}}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"s":"state-1","n":"nonce-1","v":"verifier-1"}`))
	readWith := func(sig string) (signInFlow, bool) {
		r := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
		r.AddCookie(&http.Cookie{Name: flowCookie, Value: encoded + "." + sig})
		return s.readFlow(r)
	}

	if flow, ok := readWith(hmacOver("oidc-flow:" + encoded)); !ok || flow.State != "state-1" {
		t.Errorf("a flow cookie signed for the flow reads as %+v, %v; want state-1", flow, ok)
	}
	if flow, ok := readWith(hmacOver("session:" + encoded)); ok {
		t.Errorf("a flow cookie signed for a session reads as %+v", flow)
	}
}
