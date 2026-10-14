package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultSMTPTimeout = 15 * time.Second

// EmailService sends transactional email over SMTP (Gmail by default).
type EmailService struct {
	host       string
	port       int
	username   string
	password   string
	from       mail.Address
	appBaseURL string
}

// NewEmailService reads its configuration from environment variables.
//
// Required: SMTP_USERNAME, SMTP_PASSWORD, APP_BASE_URL.
// Optional: SMTP_HOST (default smtp.gmail.com), SMTP_PORT (default 587;
// 465 uses implicit TLS, anything else uses STARTTLS), SMTP_FROM
// (default SMTP_USERNAME; may be "Name <address>").
//
// For Gmail, SMTP_PASSWORD must be an App Password, not the account password.
func NewEmailService() (*EmailService, error) {
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	if host == "" {
		host = "smtp.gmail.com"
	}

	port := 587
	if raw := strings.TrimSpace(os.Getenv("SMTP_PORT")); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil || p < 1 || p > 65535 {
			return nil, errors.New("SMTP_PORT must be a valid port number")
		}
		port = p
	}

	username := strings.TrimSpace(os.Getenv("SMTP_USERNAME"))
	// Google shows app passwords in groups separated by spaces; drop them.
	password := strings.ReplaceAll(strings.TrimSpace(os.Getenv("SMTP_PASSWORD")), " ", "")
	if username == "" {
		return nil, errors.New("SMTP_USERNAME is not configured")
	}
	if password == "" {
		return nil, errors.New("SMTP_PASSWORD is not configured")
	}

	fromRaw := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if fromRaw == "" {
		fromRaw = username
	}
	from, err := mail.ParseAddress(fromRaw)
	if err != nil {
		return nil, errors.New("SMTP_FROM must be an email address or \"Name <address>\"")
	}

	appBaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("APP_BASE_URL")), "/")
	if appBaseURL == "" {
		return nil, errors.New("APP_BASE_URL is not configured")
	}
	parsedURL, err := url.ParseRequestURI(appBaseURL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return nil, errors.New("APP_BASE_URL must be an absolute http or https URL")
	}

	return &EmailService{
		host:       host,
		port:       port,
		username:   username,
		password:   password,
		from:       *from,
		appBaseURL: appBaseURL,
	}, nil
}

// SendVerificationEmail sends a verification link to the supplied address.
// The raw token is only included in the email link; store its hash in the database.
func (s *EmailService) SendVerificationEmail(ctx context.Context, toEmail, displayName, rawToken string) error {
	toEmail = strings.TrimSpace(toEmail)
	if toEmail == "" || strings.TrimSpace(rawToken) == "" {
		return errors.New("recipient email and verification token are required")
	}
	to, err := mail.ParseAddress(toEmail)
	if err != nil || to.Address != toEmail {
		return errors.New("recipient email is invalid")
	}

	verificationURL := s.appBaseURL + "/verify-email.html?token=" + url.QueryEscape(rawToken)
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "there"
	}

	// Escape user-controlled values before inserting them into HTML.
	escapedName := template.HTMLEscapeString(name)
	escapedURL := template.HTMLEscapeString(verificationURL)
	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family:Arial,sans-serif;line-height:1.6;color:#171717">
  <h2>Verify your email address</h2>
  <p>Hello %s,</p>
  <p>Thanks for creating an account. Verify your email address to finish registration.</p>
  <p><a href="%s" style="display:inline-block;padding:12px 18px;background:#171717;color:#fff;text-decoration:none;border-radius:6px">Verify email</a></p>
  <p>If you did not create this account, you can ignore this email.</p>
  <p style="font-size:12px;color:#666">If the button doesn't work, copy this link into your browser:<br>%s</p>
</body>
</html>`, escapedName, escapedURL, escapedURL)

	textBody := fmt.Sprintf("Hello %s,\n\nThanks for creating an account. Verify your email address to finish registration:\n%s\n\nIf you did not create this account, you can ignore this email.\n", name, verificationURL)

	message, err := s.buildMessage(to, "Verify your email address", textBody, htmlBody)
	if err != nil {
		return fmt.Errorf("build email: %w", err)
	}
	return s.send(ctx, to.Address, message)
}

// buildMessage assembles an RFC 5322 multipart/alternative message.
func (s *EmailService) buildMessage(to *mail.Address, subject, textBody, htmlBody string) ([]byte, error) {
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	domain := "localhost"
	if at := strings.LastIndex(s.from.Address, "@"); at >= 0 {
		domain = s.from.Address[at+1:]
	}

	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)

	// Address.String() quotes and encodes names, so display names cannot inject headers.
	var head bytes.Buffer
	fmt.Fprintf(&head, "From: %s\r\n", s.from.String())
	fmt.Fprintf(&head, "To: %s\r\n", to.String())
	fmt.Fprintf(&head, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&head, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&head, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(idBytes), domain)
	fmt.Fprintf(&head, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&head, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", mp.Boundary())

	for _, part := range []struct{ contentType, body string }{
		{"text/plain; charset=UTF-8", textBody},
		{"text/html; charset=UTF-8", htmlBody},
	} {
		w, err := mp.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.contentType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(part.body)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mp.Close(); err != nil {
		return nil, err
	}

	return append(head.Bytes(), buf.Bytes()...), nil
}

// send delivers one message, honouring ctx's deadline (or a default timeout).
func (s *EmailService) send(ctx context.Context, rcpt string, message []byte) error {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > defaultSMTPTimeout {
		deadline = time.Now().Add(defaultSMTPTimeout)
	}

	addr := net.JoinHostPort(s.host, strconv.Itoa(s.port))
	dialer := &net.Dialer{Deadline: deadline}
	tlsConfig := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}

	var (
		conn net.Conn
		err  error
	)
	if s.port == 465 {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("start SMTP session: %w", err)
	}
	defer client.Close()

	if s.port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("start TLS: %w", err)
			}
		}
	}

	// PlainAuth refuses to send credentials over an unencrypted connection to a
	// non-local server, so the password cannot leak if TLS was not negotiated.
	if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
		return fmt.Errorf("SMTP authentication failed: %w", err)
	}
	if err := client.Mail(s.from.Address); err != nil {
		return fmt.Errorf("SMTP sender rejected: %w", err)
	}
	if err := client.Rcpt(rcpt); err != nil {
		return fmt.Errorf("SMTP recipient rejected: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP data: %w", err)
	}
	if _, err := w.Write(message); err != nil {
		return fmt.Errorf("SMTP write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP send: %w", err)
	}
	_ = client.Quit()
	return nil
}
