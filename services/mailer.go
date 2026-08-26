package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"
	"time"
)

type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

type brandedEmail struct {
	Preheader   string
	Eyebrow     string
	Title       string
	BodyHTML    string
	ActionLabel string
	ActionURL   string
	Footnote    string
	HeroURL     string
}

func sendResend(ctx context.Context, payload resendEmail, idempotencyKey string) error {
	apiKey := strings.TrimSpace(os.Getenv("RESEND_API_KEY"))
	from := strings.TrimSpace(os.Getenv("RESEND_FROM_EMAIL"))
	if apiKey == "" || from == "" {
		return fmt.Errorf("resend is not configured")
	}
	payload.From = from
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode resend email: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pitchtz-api/1.0")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("send resend email: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("resend returned status %d", response.StatusCode)
	}
	return nil
}

// renderPitchTZEmail uses table layout and inline styles so the message stays
// polished in Gmail and Outlook. The optional remote hero is decorative: when
// a mailbox blocks images, every important word and action remains visible.
func renderPitchTZEmail(mail brandedEmail) string {
	hero := ""
	if strings.TrimSpace(mail.HeroURL) != "" {
		hero = fmt.Sprintf(`<tr><td style="padding:0;background:#123d2c"><img src="%s" width="600" alt="PitchTZ sports venue" style="display:block;width:100%%;max-width:600px;height:190px;object-fit:cover;border:0" /></td></tr>`, html.EscapeString(mail.HeroURL))
	}
	return fmt.Sprintf(`<!doctype html><html lang="en"><body style="margin:0;padding:0;background:#f3f1e9;color:#17201a">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent">%s</div>
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#f3f1e9;padding:28px 12px"><tr><td align="center">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="max-width:600px;background:#ffffff;border:1px solid #deddd5;border-radius:24px;overflow:hidden">
<tr><td style="background:#0e3b2c;padding:24px 30px"><table role="presentation" width="100%%"><tr><td style="font-family:Arial,Helvetica,sans-serif;font-size:23px;font-weight:800;letter-spacing:-.5px;color:#ffffff">PITCH<span style="color:#c9f24e">TZ</span></td><td align="right" style="font-family:Arial,Helvetica,sans-serif;font-size:10px;font-weight:700;letter-spacing:1.8px;color:#a9bdb2">TAFUTA · WEKA NAFASI · CHEZA</td></tr></table></td></tr>
%s
<tr><td style="padding:34px 34px 14px;font-family:Arial,Helvetica,sans-serif"><div style="font-size:11px;font-weight:800;letter-spacing:1.6px;color:#3e7c5b;text-transform:uppercase">%s</div><h1 style="margin:9px 0 14px;font-size:29px;line-height:1.14;letter-spacing:-.8px;color:#17201a">%s</h1><div style="font-size:15px;line-height:1.7;color:#59635d">%s</div></td></tr>
<tr><td style="padding:12px 34px 26px"><a href="%s" style="display:inline-block;background:#0e3b2c;color:#ffffff;text-decoration:none;border-radius:999px;padding:14px 22px;font-family:Arial,Helvetica,sans-serif;font-size:14px;font-weight:800">%s&nbsp;&nbsp;→</a></td></tr>
<tr><td style="padding:0 34px 30px;font-family:Arial,Helvetica,sans-serif;font-size:12px;line-height:1.65;color:#7a827c">%s</td></tr>
<tr><td style="background:#f8f7f2;border-top:1px solid #e7e5dd;padding:22px 34px;font-family:Arial,Helvetica,sans-serif"><div style="font-size:14px;font-weight:800;color:#17201a">Ben</div><div style="font-size:12px;color:#68736c;padding-top:3px">Maker of PitchTZ · Dar es Salaam, Tanzania</div></td></tr>
</table><div style="padding:16px;font-family:Arial,Helvetica,sans-serif;font-size:10px;line-height:1.5;color:#8a918c">PitchTZ transactional email. Please never share a password or secure link with anyone.</div>
</td></tr></table></body></html>`, html.EscapeString(mail.Preheader), hero, html.EscapeString(mail.Eyebrow), html.EscapeString(mail.Title), mail.BodyHTML, html.EscapeString(mail.ActionURL), html.EscapeString(mail.ActionLabel), html.EscapeString(mail.Footnote))
}

func emailHeroURL() string {
	if value := strings.TrimSpace(os.Getenv("EMAIL_HERO_URL")); value != "" {
		return value
	}
	return "https://pitchtz.flutterai.dev/images/pitchtz-twin-pitches-v2.png"
}

func SendPasswordReset(ctx context.Context, to, name, resetURL, audience, idempotencyKey string) error {
	portal := "Venue House"
	if audience == "admin" {
		portal = "Superadmin"
	}
	if strings.TrimSpace(name) == "" {
		name = "there"
	}
	body := fmt.Sprintf(`<p style="margin:0 0 12px">Hello <strong style="color:#17201a">%s</strong>, we received a request to reset your PitchTZ %s password.</p><p style="margin:0">Bonyeza kitufe hapa chini kuweka neno jipya la siri. Kiungo hiki ni cha matumizi moja tu.</p>`, html.EscapeString(name), html.EscapeString(portal))
	payload := resendEmail{To: []string{to}, Subject: "Reset your PitchTZ password",
		Text: fmt.Sprintf("Hello %s,\n\nReset your PitchTZ %s password:\n%s\n\nThis one-time link expires in 30 minutes. If you did not request it, ignore this email.\n\nBen\nMaker of PitchTZ", name, portal, resetURL),
		HTML: renderPitchTZEmail(brandedEmail{Preheader: "Your secure PitchTZ password reset link.", Eyebrow: "Secure account access", Title: "Choose a new password.", BodyHTML: body, ActionLabel: "Reset password", ActionURL: resetURL, Footnote: "This secure link expires in 30 minutes and works once. If you did not make this request, you can safely ignore this email.", HeroURL: emailHeroURL()}),
	}
	return sendResend(ctx, payload, idempotencyKey)
}

// SendWelcomeAccess welcomes a provisioned owner or staff member. These are
// invited accounts only; the temporary password must be changed after login.
func SendWelcomeAccess(ctx context.Context, to, name, temporaryPassword, portalURL, audience, idempotencyKey string) error {
	if strings.TrimSpace(name) == "" {
		name = "there"
	}
	portalName := "Venue House"
	title := "Your venue workspace is ready."
	subject := "Welcome to PitchTZ Venue House"
	intro := "Your venue application has been approved. You can now manage bookings, live slots, gallery and payouts from one place."
	if audience == "admin" {
		portalName = "Superadmin"
		title = "Welcome to PitchTZ mission control."
		subject = "Welcome to PitchTZ Superadmin"
		intro = "Your staff account is ready. Your role and permissions are managed by the PitchTZ Superadmin team."
	}
	body := fmt.Sprintf(`<p style="margin:0 0 14px">Hello <strong style="color:#17201a">%s</strong>, %s</p><p style="margin:0 0 14px">Karibu PitchTZ — tunafurahi kuwa nawe.</p><table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#f5f7f1;border:1px solid #dce3d6;border-radius:14px"><tr><td style="padding:16px 18px;font-family:Arial,Helvetica,sans-serif;font-size:12px;color:#68736c">PORTAL<br><strong style="font-size:14px;color:#17201a">%s</strong></td></tr><tr><td style="padding:0 18px 16px;font-family:Arial,Helvetica,sans-serif;font-size:12px;color:#68736c">TEMPORARY PASSWORD<br><strong style="font-family:'Courier New',monospace;font-size:16px;letter-spacing:.5px;color:#0e3b2c">%s</strong></td></tr></table>`, html.EscapeString(name), html.EscapeString(intro), html.EscapeString(portalName), html.EscapeString(temporaryPassword))
	payload := resendEmail{To: []string{to}, Subject: subject,
		Text: fmt.Sprintf("Hello %s,\n\n%s\n\nPortal: %s\nTemporary password: %s\nSign in: %s\n\nYou will be asked to choose a new password.\n\nKaribu PitchTZ.\nBen\nMaker of PitchTZ", name, intro, portalName, temporaryPassword, portalURL),
		HTML: renderPitchTZEmail(brandedEmail{Preheader: "Your invited PitchTZ account is ready.", Eyebrow: "Welcome to PitchTZ", Title: title, BodyHTML: body, ActionLabel: "Open " + portalName, ActionURL: portalURL, Footnote: "For your security, sign in with the temporary password and replace it immediately. This account was created by invitation; PitchTZ never enables public staff signup.", HeroURL: emailHeroURL()}),
	}
	return sendResend(ctx, payload, idempotencyKey)
}
