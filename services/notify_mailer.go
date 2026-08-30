package services

import (
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"strings"
	"time"

	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/google/uuid"
)

// adminNotifyEmail is where platform event copies go. NOTIFY_ADMIN_EMAIL wins;
// otherwise the bootstrap superadmin address is used.
func adminNotifyEmail() string {
	if value := strings.TrimSpace(os.Getenv("NOTIFY_ADMIN_EMAIL")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL"))
}

func ownerAppURL() string {
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv("OWNER_APP_URL")), "/"); value != "" {
		return value
	}
	return "http://localhost:3002"
}

func adminAppURL() string {
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv("ADMIN_APP_URL")), "/"); value != "" {
		return value
	}
	return "http://localhost:3001"
}

func formatTZS(amount int64) string {
	digits := fmt.Sprintf("%d", amount)
	var out []byte
	for i, ch := range []byte(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, ch)
	}
	return "TZS " + string(out)
}

func factRows(pairs [][2]string) string {
	var rows strings.Builder
	for _, pair := range pairs {
		rows.WriteString(fmt.Sprintf(`<tr><td style="padding:9px 20px 3px;font-family:Arial,Helvetica,sans-serif;font-size:10px;font-weight:700;letter-spacing:1.4px;color:#a9bdb2">%s</td></tr><tr><td style="padding:0 20px;font-family:Arial,Helvetica,sans-serif;font-size:15px;font-weight:700;color:#ffffff">%s</td></tr>`,
			html.EscapeString(strings.ToUpper(pair[0])), html.EscapeString(pair[1])))
	}
	return fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#0e3b2c;border-radius:16px;margin:6px 0 4px"><tr><td style="padding-top:9px"></td></tr>%s<tr><td style="padding-bottom:18px"></td></tr></table>`, rows.String())
}

func sendBranded(ctx context.Context, to, subject, text string, mail brandedEmail, idempotencyKey string) {
	if strings.TrimSpace(to) == "" {
		return
	}
	mail.HeroURL = emailHeroURL()
	payload := resendEmail{To: []string{to}, Subject: subject, Text: text, HTML: renderPitchTZEmail(mail)}
	if err := sendResend(ctx, payload, idempotencyKey); err != nil {
		log.Printf("notify email %q to %s failed: %v", subject, to, err)
	}
}

// SendVenueApprovedEmails congratulates the owner the moment their venue goes
// live, with a copy to the superadmin inbox. Always sent on approval — social
// sign-up owners have no temporary password, but they still deserve the news.
func SendVenueApprovedEmails(ctx context.Context, ownerEmail, ownerName, venueName string, venueID uuid.UUID) {
	if strings.TrimSpace(ownerName) == "" {
		ownerName = "there"
	}
	body := fmt.Sprintf(`<p style="margin:0 0 14px">Hello <strong style="color:#17201a">%s</strong>, great news — <strong style="color:#17201a">%s</strong> has been reviewed and is now <strong style="color:#3e7c5b">live on PitchTZ</strong>.</p><p style="margin:0 0 14px">Hongera! Uwanja wako sasa uko hewani.</p><p style="margin:0">Next step: open your Venue House and add your pitches — each field with its format, hourly price and a photo. Players can only book what you list.</p>`,
		html.EscapeString(ownerName), html.EscapeString(venueName))
	sendBranded(ctx, ownerEmail, "Your venue is live on PitchTZ 🎉",
		fmt.Sprintf("Hello %s,\n\nGreat news — %s has been approved and is now live on PitchTZ.\n\nNext step: open your Venue House (%s) and add your pitches so players can start booking.\n\nHongera!\nBen\nPitchTZ Founder", ownerName, venueName, ownerAppURL()),
		brandedEmail{Preheader: venueName + " has been approved and is live.", Eyebrow: "Venue approved", Title: "You're live. Let's fill the calendar.", BodyHTML: body, ActionLabel: "Set up your pitches", ActionURL: ownerAppURL(), Footnote: "You are receiving this because your venue application on PitchTZ was approved."},
		"venue-approved-owner-"+venueID.String())

	if admin := adminNotifyEmail(); admin != "" && !strings.EqualFold(admin, ownerEmail) {
		adminBody := fmt.Sprintf(`<p style="margin:0 0 12px"><strong style="color:#17201a">%s</strong> was approved and is now live.</p>%s`,
			html.EscapeString(venueName), factRows([][2]string{{"Venue", venueName}, {"Owner", ownerName}, {"Owner email", ownerEmail}}))
		sendBranded(ctx, admin, "Venue approved: "+venueName,
			fmt.Sprintf("Venue approved and live: %s\nOwner: %s <%s>\n\nSuperadmin: %s", venueName, ownerName, ownerEmail, adminAppURL()),
			brandedEmail{Preheader: "Venue approved: " + venueName, Eyebrow: "Platform event", Title: "A venue just went live.", BodyHTML: adminBody, ActionLabel: "Open Superadmin", ActionURL: adminAppURL(), Footnote: "Superadmin copy of a platform event."},
			"venue-approved-admin-"+venueID.String())
	}
}

// SendPitchLiveEmails confirms a newly published pitch to the owner and copies
// the superadmin so the team sees supply growing in real time.
func SendPitchLiveEmails(ctx context.Context, ownerEmail, ownerName, venueName, pitchName, format string, priceTZS int64, pitchID uuid.UUID) {
	if strings.TrimSpace(ownerName) == "" {
		ownerName = "there"
	}
	facts := factRows([][2]string{{"Pitch", pitchName}, {"Venue", venueName}, {"Format", format}, {"Price per hour", formatTZS(priceTZS)}})
	body := fmt.Sprintf(`<p style="margin:0 0 14px">Hello <strong style="color:#17201a">%s</strong>, your pitch is published and players can now book it.</p>%s<p style="margin:14px 0 0">Kiwanja chako sasa kinaonekana kwa wachezaji wote.</p>`,
		html.EscapeString(ownerName), facts)
	sendBranded(ctx, ownerEmail, fmt.Sprintf("%s is now bookable on PitchTZ", pitchName),
		fmt.Sprintf("Hello %s,\n\nYour pitch is live:\n%s at %s\nFormat: %s\nPrice: %s/hour\n\nManage it anytime: %s\n\nBen\nPitchTZ Founder", ownerName, pitchName, venueName, format, formatTZS(priceTZS), ownerAppURL()),
		brandedEmail{Preheader: pitchName + " is published and bookable.", Eyebrow: "Pitch published", Title: "Your pitch is open for bookings.", BodyHTML: body, ActionLabel: "Open Venue House", ActionURL: ownerAppURL(), Footnote: "You are receiving this because a pitch was published on your PitchTZ venue."},
		"pitch-live-owner-"+pitchID.String())

	if admin := adminNotifyEmail(); admin != "" && !strings.EqualFold(admin, ownerEmail) {
		adminBody := fmt.Sprintf(`<p style="margin:0 0 12px">A new pitch was published.</p>%s`, factRows([][2]string{{"Pitch", pitchName}, {"Venue", venueName}, {"Owner", ownerName + " <" + ownerEmail + ">"}, {"Format", format}, {"Price per hour", formatTZS(priceTZS)}}))
		sendBranded(ctx, admin, fmt.Sprintf("New pitch live: %s @ %s", pitchName, venueName),
			fmt.Sprintf("New pitch published:\n%s at %s (%s, %s/hour)\nOwner: %s <%s>", pitchName, venueName, format, formatTZS(priceTZS), ownerName, ownerEmail),
			brandedEmail{Preheader: "New pitch published: " + pitchName, Eyebrow: "Platform event", Title: "Supply is growing.", BodyHTML: adminBody, ActionLabel: "Open Superadmin", ActionURL: adminAppURL(), Footnote: "Superadmin copy of a platform event."},
			"pitch-live-admin-"+pitchID.String())
	}
}

// NotifyBookingConfirmed emails everyone with a stake in a fully paid booking:
// the player (receipt), the venue owner (heads-up) and the superadmin (copy).
// Runs in its own context so a slow mail API never blocks settlement.
func NotifyBookingConfirmed(bookingID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var booking models.Booking
	if err := initializers.DB.WithContext(ctx).First(&booking, "id = ?", bookingID).Error; err != nil {
		return
	}
	var pitch models.Pitch
	if err := initializers.DB.WithContext(ctx).First(&pitch, "id = ?", booking.PitchID).Error; err != nil {
		return
	}
	var venue models.Venue
	if err := initializers.DB.WithContext(ctx).First(&venue, "id = ?", pitch.VenueID).Error; err != nil {
		return
	}
	var owner models.User
	initializers.DB.WithContext(ctx).First(&owner, "id = ?", venue.OwnerID)

	customerName, customerEmail := "Player", ""
	if booking.UserID != nil {
		var customer models.User
		if err := initializers.DB.WithContext(ctx).First(&customer, "id = ?", *booking.UserID).Error; err == nil {
			customerName = customer.Name
			if customer.Email != nil {
				customerEmail = *customer.Email
			}
		}
	}

	inDar := booking.StartsAt.In(eatZone())
	when := inDar.Format("Mon, 02 Jan 2006 · 15:04") + "–" + booking.EndsAt.In(eatZone()).Format("15:04")
	var paidTotal int64
	initializers.DB.WithContext(ctx).Model(&models.PaymentShare{}).
		Where("booking_id = ? AND status = ?", booking.ID, "paid").
		Select("COALESCE(SUM(amount_tzs), 0)").Scan(&paidTotal)
	facts := [][2]string{{"Booking code", booking.Code}, {"Venue", venue.Name}, {"Pitch", pitch.Name}, {"When (EAT)", when}, {"Paid online", formatTZS(paidTotal)}}
	if booking.BalanceAtVenue && paidTotal < booking.TotalTZS {
		facts = append(facts, [2]string{"Balance due at the gate", formatTZS(booking.TotalTZS - paidTotal)})
	}

	// Push lands on the phone the moment the money settles — the email is the
	// receipt, this is the "you're in" the player actually feels.
	if booking.UserID != nil {
		go SendToUser(*booking.UserID,
			Text{EN: "Booking confirmed ⚽", SW: "Booking imethibitishwa ⚽"},
			Text{
				EN: fmt.Sprintf("%s at %s · %s. Code %s", pitch.Name, venue.Name, when, booking.Code),
				SW: fmt.Sprintf("%s katika %s · %s. Namba %s", pitch.Name, venue.Name, when, booking.Code),
			},
			map[string]string{"type": "booking_confirmed", "booking_id": booking.ID.String(), "code": booking.Code})
	}

	if customerEmail != "" {
		body := fmt.Sprintf(`<p style="margin:0 0 14px">Hello <strong style="color:#17201a">%s</strong>, your booking is confirmed and fully paid. Show the booking code at the gate.</p>%s<p style="margin:14px 0 0">Karibu uwanjani — mchezo mzuri!</p>`,
			html.EscapeString(customerName), factRows(facts))
		sendBranded(ctx, customerEmail, "Booking confirmed: "+booking.Code+" · "+venue.Name,
			fmt.Sprintf("Hello %s,\n\nYour booking is confirmed.\n\nCode: %s\nVenue: %s\nPitch: %s\nWhen: %s (EAT)\nTotal paid: %s\n\nShow the booking code at the gate. Karibu uwanjani!\n\nBen\nPitchTZ Founder", customerName, booking.Code, venue.Name, pitch.Name, when, formatTZS(booking.TotalTZS)),
			brandedEmail{Preheader: "Confirmed: " + venue.Name + " · " + when, Eyebrow: "Booking confirmed", Title: "You're booked. Game on.", BodyHTML: body, ActionLabel: "View my booking", ActionURL: clientAppURL(), Footnote: "This is your receipt for a PitchTZ booking. Show the booking code at the venue gate."},
			"booking-confirmed-customer-"+booking.ID.String())
	}

	if owner.Email != nil && *owner.Email != "" {
		ownerFacts := append([][2]string{}, facts...)
		ownerFacts = append(ownerFacts, [2]string{"Player", customerName})
		body := fmt.Sprintf(`<p style="margin:0 0 14px">Hello <strong style="color:#17201a">%s</strong>, a booking at <strong style="color:#17201a">%s</strong> was just paid in full.</p>%s`,
			html.EscapeString(owner.Name), html.EscapeString(venue.Name), factRows(ownerFacts))
		sendBranded(ctx, *owner.Email, "New paid booking: "+booking.Code+" · "+pitch.Name,
			fmt.Sprintf("Hello %s,\n\nNew fully paid booking at %s.\n\nCode: %s\nPitch: %s\nWhen: %s (EAT)\nPlayer: %s\nTotal paid: %s\n\nSee it in Venue House: %s\n\nBen\nPitchTZ Founder", owner.Name, venue.Name, booking.Code, pitch.Name, when, customerName, formatTZS(booking.TotalTZS), ownerAppURL()),
			brandedEmail{Preheader: "Paid booking " + booking.Code + " · " + when, Eyebrow: "New paid booking", Title: "Money in — slot confirmed.", BodyHTML: body, ActionLabel: "Open Venue House", ActionURL: ownerAppURL(), Footnote: "You are receiving this because a booking at your venue was paid in full."},
			"booking-confirmed-owner-"+booking.ID.String())
	}

	if admin := adminNotifyEmail(); admin != "" {
		adminFacts := append([][2]string{}, facts...)
		adminFacts = append(adminFacts, [2]string{"Player", customerName})
		body := fmt.Sprintf(`<p style="margin:0 0 12px">A booking was paid in full.</p>%s`, factRows(adminFacts))
		sendBranded(ctx, admin, "Paid booking: "+booking.Code+" · "+venue.Name,
			fmt.Sprintf("Paid booking %s\nVenue: %s\nPitch: %s\nWhen: %s (EAT)\nPlayer: %s\nTotal: %s", booking.Code, venue.Name, pitch.Name, when, customerName, formatTZS(booking.TotalTZS)),
			brandedEmail{Preheader: "Paid booking " + booking.Code, Eyebrow: "Platform event", Title: "GMV is moving.", BodyHTML: body, ActionLabel: "Open Superadmin", ActionURL: adminAppURL(), Footnote: "Superadmin copy of a platform event."},
			"booking-confirmed-admin-"+booking.ID.String())
	}
}

func eatZone() *time.Location {
	if zone, err := time.LoadLocation("Africa/Dar_es_Salaam"); err == nil {
		return zone
	}
	return time.FixedZone("EAT", 3*3600)
}

// SendSignupNoticeEmail tells the superadmin a new person joined, and through
// which door: client site, venue house, or a venue application.
func SendSignupNoticeEmail(name, email, portal, provider string, userID uuid.UUID) {
	admin := adminNotifyEmail()
	if admin == "" || strings.EqualFold(admin, email) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if strings.TrimSpace(name) == "" {
		name = strings.Split(email, "@")[0]
	}
	body := fmt.Sprintf(`<p style="margin:0 0 12px">A new account was just created.</p>%s`,
		factRows([][2]string{{"Name", name}, {"Email", email}, {"Portal", portal}, {"Method", provider}}))
	sendBranded(ctx, admin, fmt.Sprintf("New sign-up (%s): %s", portal, name),
		fmt.Sprintf("New account:\n%s <%s>\nPortal: %s\nMethod: %s", name, email, portal, provider),
		brandedEmail{Preheader: "New sign-up: " + name, Eyebrow: "Platform event", Title: "The community is growing.", BodyHTML: body, ActionLabel: "Open Superadmin", ActionURL: adminAppURL(), Footnote: "Superadmin copy of a platform event."},
		"signup-"+userID.String())
}

// SendVenueApplicationEmail tells the superadmin a new venue application
// arrived and needs review.
func SendVenueApplicationEmail(venueName, area, ownerName, ownerEmail string, venueID uuid.UUID) {
	admin := adminNotifyEmail()
	if admin == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	body := fmt.Sprintf(`<p style="margin:0 0 12px">A new venue application is waiting for review.</p>%s`,
		factRows([][2]string{{"Venue", venueName}, {"Area", area}, {"Owner", ownerName}, {"Owner email", ownerEmail}}))
	sendBranded(ctx, admin, "New venue application: "+venueName,
		fmt.Sprintf("New venue application:\n%s (%s)\nOwner: %s <%s>\n\nReview it: %s", venueName, area, ownerName, ownerEmail, adminAppURL()),
		brandedEmail{Preheader: "Venue application: " + venueName, Eyebrow: "Needs review", Title: "A venue wants to join.", BodyHTML: body, ActionLabel: "Review in Superadmin", ActionURL: adminAppURL(), Footnote: "Superadmin copy of a platform event."},
		"venue-application-"+venueID.String())
}

func teamsURL() string {
	return clientAppURL() + "/teams"
}

// SendTeamJoinRequestEmail: a player knocked — the captain decides.
func SendTeamJoinRequestEmail(captainEmail, captainName, playerName, teamName string, teamID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if strings.TrimSpace(captainName) == "" {
		captainName = "Captain"
	}
	body := fmt.Sprintf(`<p style="margin:0 0 12px">Hello <strong style="color:#17201a">%s</strong>, <strong style="color:#17201a">%s</strong> wants to join <strong style="color:#17201a">%s</strong>.</p><p style="margin:0">Open your team page to accept or decline. Timu imara huanza na walinzi wazuri wa mlango.</p>`,
		html.EscapeString(captainName), html.EscapeString(playerName), html.EscapeString(teamName))
	sendBranded(ctx, captainEmail, playerName+" wants to join "+teamName,
		fmt.Sprintf("Hello %s,\n\n%s wants to join %s.\n\nAccept or decline: %s\n\nBen\nPitchTZ Founder", captainName, playerName, teamName, teamsURL()),
		brandedEmail{Preheader: playerName + " asked to join " + teamName, Eyebrow: "Join request", Title: "A player wants in.", BodyHTML: body, ActionLabel: "Review request", ActionURL: teamsURL(), Footnote: "You are receiving this because you are the captain of a PitchTZ team."},
		fmt.Sprintf("team-join-%s-%s", teamID.String(), strings.ToLower(playerName)))
}

// SendTeamDecisionEmail tells the player whether they made the squad.
func SendTeamDecisionEmail(playerEmail, playerName, teamName string, accepted bool, teamID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if strings.TrimSpace(playerName) == "" {
		playerName = "there"
	}
	if accepted {
		body := fmt.Sprintf(`<p style="margin:0 0 12px">Hello <strong style="color:#17201a">%s</strong>, you're in — <strong style="color:#17201a">%s</strong> accepted your request. Karibu kwenye timu!</p>`,
			html.EscapeString(playerName), html.EscapeString(teamName))
		sendBranded(ctx, playerEmail, "You made the squad: "+teamName,
			fmt.Sprintf("Hello %s,\n\nYou're in — %s accepted your request. Karibu kwenye timu!\n\n%s\n\nBen\nPitchTZ Founder", playerName, teamName, teamsURL()),
			brandedEmail{Preheader: "Accepted into " + teamName, Eyebrow: "Welcome to the squad", Title: "You made the team.", BodyHTML: body, ActionLabel: "See your team", ActionURL: teamsURL(), Footnote: "You are receiving this because you asked to join a PitchTZ team."},
			"team-accept-"+teamID.String()+"-"+strings.ToLower(playerName))
		return
	}
	body := fmt.Sprintf(`<p style="margin:0 0 12px">Hello <strong style="color:#17201a">%s</strong>, <strong style="color:#17201a">%s</strong> could not take you on this time. Plenty of squads are recruiting — keep exploring.</p>`,
		html.EscapeString(playerName), html.EscapeString(teamName))
	sendBranded(ctx, playerEmail, "About your request to "+teamName,
		fmt.Sprintf("Hello %s,\n\n%s could not take you on this time. Plenty of squads are recruiting — keep exploring: %s\n\nBen\nPitchTZ Founder", playerName, teamName, teamsURL()),
		brandedEmail{Preheader: "Update on " + teamName, Eyebrow: "Join request", Title: "Not this time — keep playing.", BodyHTML: body, ActionLabel: "Explore other teams", ActionURL: teamsURL(), Footnote: "You are receiving this because you asked to join a PitchTZ team."},
		"team-decline-"+teamID.String()+"-"+strings.ToLower(playerName))
}

// SendChallengeAcceptedEmail goes to both captains when a game is on.
func SendChallengeAcceptedEmail(captainEmail, captainName, ownTeam, opponentTeam string, matchID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if strings.TrimSpace(captainName) == "" {
		captainName = "Captain"
	}
	body := fmt.Sprintf(`<p style="margin:0 0 12px">Hello <strong style="color:#17201a">%s</strong>, it's on — <strong style="color:#17201a">%s</strong> vs <strong style="color:#17201a">%s</strong>.</p><p style="margin:0">Agree a time with the other captain and book a pitch on PitchTZ to lock it in.</p>`,
		html.EscapeString(captainName), html.EscapeString(ownTeam), html.EscapeString(opponentTeam))
	sendBranded(ctx, captainEmail, fmt.Sprintf("Game on: %s vs %s", ownTeam, opponentTeam),
		fmt.Sprintf("Hello %s,\n\nIt's on — %s vs %s.\n\nAgree a time and book a pitch: %s\n\nBen\nPitchTZ Founder", captainName, ownTeam, opponentTeam, clientAppURL()),
		brandedEmail{Preheader: ownTeam + " vs " + opponentTeam, Eyebrow: "Challenge accepted", Title: "Game on.", BodyHTML: body, ActionLabel: "Book the pitch", ActionURL: clientAppURL(), Footnote: "You are receiving this because your team is in a PitchTZ challenge."},
		"challenge-"+matchID.String()+"-"+strings.ToLower(ownTeam))
}

// SendWatchSpotApplicationEmail tells the superadmin a bar/lounge wants on
// the watch-parties map.
func SendWatchSpotApplicationEmail(name, area, contactName, contactPhone string, spotID uuid.UUID) {
	admin := adminNotifyEmail()
	if admin == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	body := fmt.Sprintf(`<p style="margin:0 0 12px">A new watch-spot application is waiting for review.</p>%s`,
		factRows([][2]string{{"Spot", name}, {"Area", area}, {"Contact", contactName}, {"Phone", contactPhone}}))
	sendBranded(ctx, admin, "Watch spot application: "+name,
		fmt.Sprintf("Watch spot application:\n%s (%s)\nContact: %s %s\n\nReview: %s", name, area, contactName, contactPhone, adminAppURL()),
		brandedEmail{Preheader: "Watch spot application: " + name, Eyebrow: "Needs review", Title: "A new place wants the big game.", BodyHTML: body, ActionLabel: "Review in Superadmin", ActionURL: adminAppURL(), Footnote: "Superadmin copy of a platform event."},
		"watchspot-"+spotID.String())
}
