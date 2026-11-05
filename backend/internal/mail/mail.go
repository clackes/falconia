// Package mail sends the few emails Falconia needs: address verification and
// team invitations.
//
// It speaks SMTP with the standard library and nothing else. Credentials are
// never sent over a clear-text connection: with "starttls" the upgrade is
// mandatory rather than opportunistic, and net/smtp's PLAIN auth itself
// refuses to send them without TLS unless the server is on localhost.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/clackes/falconia/backend/internal/config"
)

type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// New returns nil when no mail server is configured: callers check for nil
// and fall back to showing the link to whoever can pass it on.
func New(c config.SMTPConfig) Sender {
	if c.Host == "" {
		return nil
	}
	return &smtpSender{c: c}
}

type smtpSender struct{ c config.SMTPConfig }

func (s *smtpSender) Send(ctx context.Context, to, subject, body string) error {
	// Header injection: an address or subject with a line break could add
	// recipients or headers. Addresses reaching here were validated, but the
	// check is cheap and belongs where the headers are written.
	if strings.ContainsAny(to, "\r\n") || strings.ContainsAny(subject, "\r\n") {
		return errors.New("mail: line break in a header")
	}
	addr := net.JoinHostPort(s.c.Host, strconv.Itoa(s.c.Port))
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Deadline: deadline}
	tlsCfg := &tls.Config{ServerName: s.c.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	var err error
	if s.c.Security == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, s.c.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()

	if s.c.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("mail: the server does not offer STARTTLS (set SMTP_SECURITY=tls or none)")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if s.c.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.c.Username, s.c.Password, s.c.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(addressOnly(s.c.From)); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	msg := "From: " + s.c.From + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"Auto-Submitted: auto-generated\r\n" +
		"\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// addressOnly extracts the bare address from "Name <addr>" for MAIL FROM.
func addressOnly(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j > 0 {
			return from[i+1 : i+j]
		}
	}
	return strings.TrimSpace(from)
}

// Describe is for the startup log.
func Describe(c config.SMTPConfig) string {
	if c.Host == "" {
		return "none"
	}
	return fmt.Sprintf("%s:%d (%s)", c.Host, c.Port, c.Security)
}
