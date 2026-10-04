package content

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGrantRoundTrip(t *testing.T) {
	secret := []byte("0123456789abcdef")
	g := Grant{ArtifactID: uuid.New(), Version: 3, UserID: uuid.Nil, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	tok, err := Sign(secret, g)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(secret, tok)
	if err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}
	if got.ArtifactID != g.ArtifactID || got.Version != 3 {
		t.Fatalf("grant changed in transit: %+v", got)
	}
}

func TestGrantRejectsTamperingAndExpiry(t *testing.T) {
	secret := []byte("0123456789abcdef")
	g := Grant{ArtifactID: uuid.New(), Version: 1, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	tok, _ := Sign(secret, g)

	if _, err := Verify([]byte("another-secret-xyz"), tok); err == nil {
		t.Fatal("grant verified with the wrong secret")
	}
	// Swap the payload for another artifact's, keep the signature.
	other, _ := Sign(secret, Grant{ArtifactID: uuid.New(), Version: 1, ExpiresAt: g.ExpiresAt})
	body, _, _ := strings.Cut(other, ".")
	_, sig, _ := strings.Cut(tok, ".")
	if _, err := Verify(secret, body+"."+sig); err == nil {
		t.Fatal("grant with a swapped payload verified")
	}
	expired, _ := Sign(secret, Grant{ArtifactID: g.ArtifactID, Version: 1, ExpiresAt: time.Now().Add(-time.Minute).Unix()})
	if _, err := Verify(secret, expired); err == nil {
		t.Fatal("expired grant verified")
	}
	for _, junk := range []string{"", ".", "abc", "abc.def", "%%%.%%%"} {
		if _, err := Verify(secret, junk); err == nil {
			t.Fatalf("junk %q verified", junk)
		}
	}
}

func TestInjectSDKComesFirst(t *testing.T) {
	cases := map[string]string{
		"<html><head><script>x()</script></head></html>": `<html><head><script src="/__sfa/sdk.js"></script><script>x()</script>`,
		"<HTML lang=en><body>hi</body></HTML>":           `<HTML lang=en><script src="/__sfa/sdk.js"></script><body>`,
		"<p>fragment</p>":                                `<script src="/__sfa/sdk.js"></script><p>`,
	}
	for in, prefix := range cases {
		if got := string(injectSDK([]byte(in))); !strings.HasPrefix(got, prefix) {
			t.Errorf("injectSDK(%q) = %q, want prefix %q", in, got, prefix)
		}
	}
}

func TestContentTypeNeverTrustsDeclaredHTML(t *testing.T) {
	if got := contentTypeFor("logo.png", "text/html"); !strings.HasPrefix(got, "image/png") {
		t.Fatalf("png served as %q", got)
	}
	if got := contentTypeFor("data.unknownext", "text/html"); got != "application/octet-stream" {
		t.Fatalf("unknown extension declared as HTML served as %q", got)
	}
}
