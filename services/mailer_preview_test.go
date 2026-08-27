package services

import (
	"fmt"
	"html"
	"os"
	"testing"
)

// TestRenderWelcomePreview writes the welcome email HTML to /tmp for a visual
// review of the branded template. Kept as a test so it never ships in builds.
func TestRenderWelcomePreview(t *testing.T) {
	name, to, temporaryPassword := "Neema J.", "neema@example.com", "TEMP-9F3K-PLAY-22"
	intro := "Your venue application has been approved. You can now manage bookings, live slots, gallery and payouts from one place."
	body := fmt.Sprintf(`<p style="margin:0 0 14px">Hello <strong style="color:#17201a">%s</strong>, %s</p><p style="margin:0 0 18px">Karibu PitchTZ — tunafurahi kuwa nawe.</p><table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#0e3b2c;border-radius:16px"><tr><td style="padding:18px 20px 6px;font-family:Arial,Helvetica,sans-serif;font-size:10px;font-weight:700;letter-spacing:1.4px;color:#a9bdb2">PORTAL</td></tr><tr><td style="padding:0 20px;font-family:Arial,Helvetica,sans-serif;font-size:15px;font-weight:800;color:#ffffff">%s</td></tr><tr><td style="padding:14px 20px 6px;font-family:Arial,Helvetica,sans-serif;font-size:10px;font-weight:700;letter-spacing:1.4px;color:#a9bdb2">YOUR EMAIL</td></tr><tr><td style="padding:0 20px;font-family:Arial,Helvetica,sans-serif;font-size:14px;color:#ffffff">%s</td></tr><tr><td style="padding:14px 20px 6px;font-family:Arial,Helvetica,sans-serif;font-size:10px;font-weight:700;letter-spacing:1.4px;color:#a9bdb2">TEMPORARY PASSWORD</td></tr><tr><td style="padding:0 20px 18px;font-family:'Courier New',monospace;font-size:19px;font-weight:700;letter-spacing:1px;color:#c9f24e">%s</td></tr></table><table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="margin-top:18px"><tr><td style="font-family:Arial,Helvetica,sans-serif;font-size:13px;line-height:1.7;color:#59635d"><strong style="color:#17201a">What you can do inside:</strong><br>1&#41; See live availability and bookings for every pitch<br>2&#41; Collect mobile money payments (Airtel, Mixx, HaloPesa) with QR split-pay<br>3&#41; Reply to player reviews and manage photos, pricing and payouts</td></tr></table>`, html.EscapeString(name), html.EscapeString(intro), html.EscapeString("Venue House"), html.EscapeString(to), html.EscapeString(temporaryPassword))
	rendered := renderPitchTZEmail(brandedEmail{
		Preheader:   "Your invited PitchTZ account is ready.",
		Eyebrow:     "Welcome to PitchTZ",
		Title:       "Your venue workspace is ready.",
		BodyHTML:    body,
		ActionLabel: "Open Venue House",
		ActionURL:   "https://owner.pitchtz.flutterai.dev",
		Footnote:    "For your security, sign in with the temporary password and replace it immediately.",
		HeroURL:     emailHeroURL(),
	})
	if err := os.WriteFile("/tmp/pitchtz-email-preview.html", []byte(rendered), 0o644); err != nil {
		t.Fatal(err)
	}
}
