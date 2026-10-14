package services

import (
	"context"
	"fmt"
	"html/template"
	"strings"
)

// SendInstitutionRequestAdminEmail tells the platform admin that a user asked
// for an institution to be registered.
func (s *EmailService) SendInstitutionRequestAdminEmail(ctx context.Context, adminEmail, userEmail, displayName, institutionName string, domains []string, seats int, message string) error {
	to, err := parseRecipient(adminEmail)
	if err != nil {
		return err
	}

	adminURL := s.appBaseURL + "/admin.html"
	if strings.TrimSpace(message) == "" {
		message = "(no message)"
	}
	seatsText := "not specified"
	if seats > 0 {
		seatsText = fmt.Sprintf("%d", seats)
	}
	domainText := strings.Join(domains, ", ")

	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family:Arial,sans-serif;line-height:1.6;color:#171717">
  <h2>New institution request</h2>
  <p><b>Institution:</b> %s</p>
  <p><b>Domains:</b> %s</p>
  <p><b>Seats wanted:</b> %s</p>
  <p><b>Requested by:</b> %s (%s)</p>
  <p><b>Message:</b></p>
  <p style="white-space:pre-wrap">%s</p>
  <p><a href="%s" style="display:inline-block;padding:12px 18px;background:#171717;color:#fff;text-decoration:none;border-radius:6px">Review requests</a></p>
</body>
</html>`,
		template.HTMLEscapeString(institutionName),
		template.HTMLEscapeString(domainText),
		template.HTMLEscapeString(seatsText),
		template.HTMLEscapeString(displayName),
		template.HTMLEscapeString(userEmail),
		template.HTMLEscapeString(message),
		template.HTMLEscapeString(adminURL))

	textBody := fmt.Sprintf("New institution request\n\nInstitution: %s\nDomains: %s\nSeats wanted: %s\nRequested by: %s (%s)\nMessage:\n%s\n\nReview requests: %s\n",
		institutionName, domainText, seatsText, displayName, userEmail, message, adminURL)

	msg, err := s.buildMessage(to, "New institution request", textBody, htmlBody)
	if err != nil {
		return fmt.Errorf("build email: %w", err)
	}
	return s.send(ctx, to.Address, msg)
}

// SendInstitutionDecisionEmail tells the requester whether their institution
// request was approved.
func (s *EmailService) SendInstitutionDecisionEmail(ctx context.Context, toEmail, displayName, institutionName string, domains []string, approved bool, note string) error {
	to, err := parseRecipient(toEmail)
	if err != nil {
		return err
	}

	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "there"
	}
	domainText := strings.Join(domains, ", ")

	subject := "Your institution request was not approved"
	headline := "Your institution request was not approved"
	line := fmt.Sprintf("Your request to register %s was not approved this time. You can send a new request from the institution request page.", institutionName)
	if approved {
		subject = "Your institution request was approved"
		headline = "Your institution is now on ChaosLab"
		line = fmt.Sprintf("Your request to register %s was approved. Everyone with a verified email on %s is on the institution plan from now on.", institutionName, domainText)
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
