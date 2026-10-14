package services

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/mail"
	"strings"
)

func parseRecipient(raw string) (*mail.Address, error) {
	raw = strings.TrimSpace(raw)
	to, err := mail.ParseAddress(raw)
	if err != nil || to.Address != raw {
		return nil, errors.New("recipient email is invalid")
	}
	return to, nil
}

// SendPlanRequestAdminEmail tells the platform admin that a user asked for Pro.
func (s *EmailService) SendPlanRequestAdminEmail(ctx context.Context, adminEmail, userEmail, displayName, message string) error {
	to, err := parseRecipient(adminEmail)
	if err != nil {
		return err
	}

	adminURL := s.appBaseURL + "/admin.html"
	if strings.TrimSpace(message) == "" {
		message = "(no message)"
	}

	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family:Arial,sans-serif;line-height:1.6;color:#171717">
  <h2>New Pro plan request</h2>
  <p><b>User:</b> %s (%s)</p>
  <p><b>Message:</b></p>
  <p style="white-space:pre-wrap">%s</p>
  <p><a href="%s" style="display:inline-block;padding:12px 18px;background:#171717;color:#fff;text-decoration:none;border-radius:6px">Review requests</a></p>
</body>
</html>`,
		template.HTMLEscapeString(displayName),
		template.HTMLEscapeString(userEmail),
		template.HTMLEscapeString(message),
		template.HTMLEscapeString(adminURL))

	textBody := fmt.Sprintf("New Pro plan request\n\nUser: %s (%s)\nMessage:\n%s\n\nReview requests: %s\n", displayName, userEmail, message, adminURL)

	msg, err := s.buildMessage(to, "New Pro plan request", textBody, htmlBody)
	if err != nil {
		return fmt.Errorf("build email: %w", err)
	}
	return s.send(ctx, to.Address, msg)
}

// SendPlanDecisionEmail tells a user whether their Pro request was approved.
func (s *EmailService) SendPlanDecisionEmail(ctx context.Context, toEmail, displayName string, approved bool, note string) error {
	to, err := parseRecipient(toEmail)
	if err != nil {
		return err
	}

	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "there"
	}

	subject := "Your Pro request was not approved"
	headline := "Your Pro request was not approved"
	line := "Your request for the Pro plan was not approved this time. You can send a new request from your profile page."
	if approved {
		subject = "Your Pro request was approved"
		headline = "You are now on Pro"
		line = "Your request for the Pro plan was approved. Pro features are available the next time you open the app."
	}

	noteHTML, noteText := "", ""
	if n := strings.TrimSpace(note); n != "" {
		noteHTML = "<p><b>Note from the admin:</b></p><p style=\"white-space:pre-wrap\">" + template.HTMLEscapeString(n) + "</p>"
		noteText = "\nNote from the admin:\n" + n + "\n"
	}

	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family:Arial,sans-serif;line-height:1.6;color:#171717">
  <h2>%s</h2>
  <p>Hello %s,</p>
  <p>%s</p>
  %s
</body>
</html>`, template.HTMLEscapeString(headline), template.HTMLEscapeString(name), template.HTMLEscapeString(line), noteHTML)

	textBody := fmt.Sprintf("Hello %s,\n\n%s\n%s", name, line, noteText)

	msg, err := s.buildMessage(to, subject, textBody, htmlBody)
	if err != nil {
		return fmt.Errorf("build email: %w", err)
	}
	return s.send(ctx, to.Address, msg)
}
