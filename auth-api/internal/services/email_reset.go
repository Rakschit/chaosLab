package services

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/mail"
	"net/url"
	"strings"
)

// SendPasswordResetEmail sends a password reset link to the supplied address.
// The raw token only ever appears in this email; the database stores its hash.
func (s *EmailService) SendPasswordResetEmail(ctx context.Context, toEmail, displayName, rawToken string) error {
	toEmail = strings.TrimSpace(toEmail)
	if toEmail == "" || strings.TrimSpace(rawToken) == "" {
		return errors.New("recipient email and reset token are required")
	}
	to, err := mail.ParseAddress(toEmail)
	if err != nil || to.Address != toEmail {
		return errors.New("recipient email is invalid")
	}

	resetURL := s.appBaseURL + "/reset-password.html?token=" + url.QueryEscape(rawToken)
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "there"
	}

	escapedName := template.HTMLEscapeString(name)
	escapedURL := template.HTMLEscapeString(resetURL)
	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family:Arial,sans-serif;line-height:1.6;color:#171717">
  <h2>Reset your password</h2>
  <p>Hello %s,</p>
  <p>We received a request to reset your password. This link works once and expires in 30 minutes.</p>
  <p><a href="%s" style="display:inline-block;padding:12px 18px;background:#171717;color:#fff;text-decoration:none;border-radius:6px">Reset password</a></p>
  <p>If you did not request this, you can ignore this email. Your password will not change.</p>
  <p style="font-size:12px;color:#666">If the button doesn't work, copy this link into your browser:<br>%s</p>
</body>
</html>`, escapedName, escapedURL, escapedURL)

	textBody := fmt.Sprintf("Hello %s,\n\nWe received a request to reset your password. This link works once and expires in 30 minutes:\n%s\n\nIf you did not request this, you can ignore this email. Your password will not change.\n", name, resetURL)

	message, err := s.buildMessage(to, "Reset your password", textBody, htmlBody)
	if err != nil {
		return fmt.Errorf("build email: %w", err)
	}
	return s.send(ctx, to.Address, message)
}
