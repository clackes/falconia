// Package content serves artifact bytes from an origin separate from the
// application's.
//
// The separation is not a theoretical precaution. An artifact is code written
// by a model and reviewed by nobody: if it ran on the application origin it
// could read the session token, call the API on behalf of whoever is looking
// at it, and delete their work. Served from a different origin it can do none
// of that, even if the iframe's sandbox attribute were removed by mistake in
// some future refactor. Two independent defenses — that is the point.
//
// Authorization travels in the PATH, not in a cookie:
//
//	https://content.example/c/<grant>/index.html
//	https://content.example/c/<grant>/assets/app.js
//
// Third-party cookies are blocked by almost every browser, and a cross-origin
// iframe is exactly the case they block. Putting the permission in the path
// solves that and a second problem for free: every relative URL inside the
// artifact inherits the grant by itself, without anyone rewriting the HTML.
package content

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidGrant = errors.New("invalid or expired grant")

// Grant is the permission to read the files of one version.
//
// It also carries who asked for it — uuid.Nil for an anonymous visitor of a
// shared link. That is used to put the right name next to "opened" in the
// audit trail, not to decide access, which was already decided when the grant
// was issued.
type Grant struct {
	ArtifactID uuid.UUID `json:"a"`
	Version    int       `json:"v"`
	UserID     uuid.UUID `json:"u"`
	ExpiresAt  int64     `json:"e"`
}

var enc = base64.RawURLEncoding

// Sign produces the string that goes in the path.
func Sign(secret []byte, g Grant) (string, error) {
	payload, err := json.Marshal(g)
	if err != nil {
		return "", err
	}
	body := enc.EncodeToString(payload)
	return body + "." + enc.EncodeToString(mac(secret, body)), nil
}

// Verify checks signature and expiry.
//
// The signature comparison uses hmac.Equal, not ==: a naive string comparison
// returns at the first differing byte, and that timing tells whoever measures
// it how many bytes they guessed.
func Verify(secret []byte, token string) (*Grant, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return nil, ErrInvalidGrant
	}
	want, err := enc.DecodeString(sig)
	if err != nil || !hmac.Equal(want, mac(secret, body)) {
		return nil, ErrInvalidGrant
	}
	payload, err := enc.DecodeString(body)
	if err != nil {
		return nil, ErrInvalidGrant
	}
	var g Grant
	if err := json.Unmarshal(payload, &g); err != nil {
		return nil, ErrInvalidGrant
	}
	if time.Now().Unix() > g.ExpiresAt {
		return nil, ErrInvalidGrant
	}
	return &g, nil
}

func mac(secret []byte, body string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(body))
	return h.Sum(nil)
}
