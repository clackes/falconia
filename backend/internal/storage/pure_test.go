package storage

import "testing"

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"index.html":          "index.html",
		"./index.html":        "index.html",
		"/assets/app.js":      "assets/app.js",
		`assets\img\logo.png`: "assets/img/logo.png",
		"a/./b/../c.css":      "a/c.css",
		"../../etc/passwd":    "",
		"..":                  "",
		"a/../../b":           "",
		"":                    "",
		"   ":                 "",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidVisibilityDefaultsClosed(t *testing.T) {
	cases := map[string]string{
		"private": "private", "restricted": "private", "shared": "private", "": "private",
		"typo": "private", "org": "org", "team": "org", "TEAM": "org",
		"link": "link", "public": "link", "anyone": "link", "anyone-with-link": "link",
	}
	for in, want := range cases {
		if got := ValidVisibility(in); got != want {
			t.Errorf("ValidVisibility(%q) = %q, want %q", in, got, want)
		}
	}
	if ValidTeamRole("viewer") != "viewer" || ValidTeamRole("") != "editor" || ValidTeamRole("owner") != "editor" {
		t.Fatal("ValidTeamRole")
	}
}

func TestLinkPathOnlyWhenShared(t *testing.T) {
	tok := "abc123"
	a := &Artifact{Visibility: "link", LinkToken: &tok}
	if a.LinkPath() != "/p/abc123" {
		t.Fatalf("LinkPath = %q", a.LinkPath())
	}
	a.Visibility = "private"
	if a.LinkPath() != "" {
		t.Fatal("a private artifact must not expose its link")
	}
}

func TestIsAPIKey(t *testing.T) {
	for _, k := range []string{"falconia_pat_abc_def", "sfa_pat_abc_def"} {
		if !IsAPIKey(k) {
			t.Errorf("%q not recognized as a key", k)
		}
	}
	if IsAPIKey("eyJhbGciOiJIUzI1NiJ9.x.y") {
		t.Error("a JWT recognized as a key")
	}
}

func TestNormalizeSlug(t *testing.T) {
	cases := map[string]string{
		"Q3 Report": "q3-report", "  Hello__World  ": "hello-world", "🚀": "", "a.b.c": "a-b-c",
	}
	for in, want := range cases {
		if got := normalizeSlug(in); got != want {
			t.Errorf("normalizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
