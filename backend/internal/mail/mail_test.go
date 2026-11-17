package mail

import (
	"context"
	"testing"

	"github.com/clackes/falconia/backend/internal/config"
)

func TestNewWithoutHost(t *testing.T) {
	if New(config.SMTPConfig{}) != nil {
		t.Fatal("a sender without a host")
	}
}

func TestHeaderInjectionRefused(t *testing.T) {
	s := &smtpSender{c: config.SMTPConfig{Host: "127.0.0.1", Port: 1}}
	if err := s.Send(context.Background(), "a@b\r\nBcc: x@y", "hi", "body"); err == nil {
		t.Fatal("a recipient with a line break was accepted")
	}
}

func TestAddressOnly(t *testing.T) {
	for in, want := range map[string]string{
		"Falconia <noreply@example.com>": "noreply@example.com",
		"noreply@example.com":            "noreply@example.com",
	} {
		if got := addressOnly(in); got != want {
			t.Errorf("%q: got %q", in, got)
		}
	}
}
