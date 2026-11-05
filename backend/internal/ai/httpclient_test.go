package ai

import "testing"

func TestRefuseForbiddenAddr(t *testing.T) {
	cases := map[string]bool{
		"169.254.169.254:80":          false,
		"[fd00:ec2::254]:80":          false,
		"[fe80::1]:80":                false,
		"0.0.0.0:80":                  false,
		"[::ffff:169.254.169.254]:80": false,
		"127.0.0.1:11434":             true,
		"10.0.0.5:8000":               true,
		"93.184.216.34:443":           true,
	}
	for addr, allowed := range cases {
		err := refuseForbiddenAddr("tcp", addr, nil)
		if (err == nil) != allowed {
			t.Errorf("%s: allowed=%v, err=%v", addr, allowed, err)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	good := map[string]string{
		"http://localhost:11434/v1/": "http://localhost:11434/v1",
		" https://llm.example/v1 ":   "https://llm.example/v1",
	}
	for in, want := range good {
		got, err := NormalizeBaseURL(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"file:///etc/passwd", "localhost:11434", "http://u:p@h/v1", "http://h/v1?x=", "http://h/v1#f", ""} {
		if _, err := NormalizeBaseURL(in); err == nil {
			t.Errorf("%q: accepted", in)
		}
	}
}
