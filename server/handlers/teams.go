package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/server/middleware"
	"github.com/godopetza/pitchtz/services"
	"github.com/godopetza/pitchtz/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Teams: football finds its people. Players create teams, solo players find
// one near them and ask to join, captains approve, and open challenges turn
// into real matches.

const (
	memberStatusRequested = "requested"
	memberStatusActive    = "active"
	memberRoleCaptain     = "captain"
	memberRolePlayer      = "player"
)

type teamCardDTO struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Tag         string    `json:"tag"`
	City        string    `json:"city"`
	Area        string    `json:"area"`
	Bio         string    `json:"bio"`
	BadgeColor  string    `json:"badge_color"`
	Format      string    `json:"format"`
	Recruiting  bool      `json:"recruiting"`
	Needs       string    `json:"needs"`
	Members     int64     `json:"members"`
	IsCaptain   bool      `json:"is_captain,omitempty"`
	Membership  string    `json:"membership,omitempty"`
	OpenMatches int64     `json:"open_challenges"`
}

// viewerID reads the optional client identity without writing any error —
// explore pages personalise when signed in and still work anonymously.
func viewerID(c *gin.Context) uuid.UUID {
	value, exists := c.Get(middleware.ClientUserIDKey)
	if !exists {
		return uuid.Nil
	}
	if id, ok := value.(uuid.UUID); ok {
		return id
	}
	return uuid.Nil
}

func teamCard(c *gin.Context, team models.Team, cityName string, viewer uuid.UUID) teamCardDTO {
	var members int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.TeamMember{}).
		Where("team_id = ? AND status = ?", team.ID, memberStatusActive).Count(&members)
	var open int64
	initializers.DB.WithContext(c.Request.Context()).Model(&models.Challenge{}).
		Where("team_id = ? AND status = 'open'", team.ID).Count(&open)
	card := teamCardDTO{
		ID: team.ID, Name: team.Name, Tag: team.Tag, City: cityName, Area: team.Area,
		Bio: team.Bio, BadgeColor: team.BadgeColor, Format: team.Format,
		Recruiting: team.Recruiting, Needs: team.Needs, Members: members, OpenMatches: open,
	}
	if viewer != uuid.Nil {
		card.IsCaptain = team.CaptainID == viewer
		var membership models.TeamMember
		if initializers.DB.WithContext(c.Request.Context()).
			First(&membership, "team_id = ? AND user_id = ?", team.ID, viewer).Error == nil {
			card.Membership = membership.Status
		}
	}
	return card
}

// ListTeams is the public explore board: filter by city, format, recruiting,
// or free-text search. Auth optional — with a token, membership is included.
func ListTeams(c *gin.Context) {
	viewer := viewerID(c)
	query := initializers.DB.WithContext(c.Request.Context()).Model(&models.Team{})
	if cityID := strings.TrimSpace(c.Query("city_id")); cityID != "" {
		query = query.Where("city_id = ?", cityID)
	}
	if format := strings.TrimSpace(c.Query("format")); format != "" {
		query = query.Where("format = ?", format)
	}
	if c.Query("recruiting") == "true" {
		query = query.Where("recruiting = true")
	}
	if search := strings.TrimSpace(c.Query("q")); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(area) LIKE ? OR LOWER(tag) LIKE ?", like, like, like)
	}
	var teams []models.Team
	query.Order("created_at DESC").Limit(60).Find(&teams)

	cityNames := map[uuid.UUID]string{}
	var cities []models.City
	initializers.DB.WithContext(c.Request.Context()).Find(&cities)
	for _, city := range cities {
		cityNames[city.ID] = city.Name
	}
	cards := make([]teamCardDTO, 0, len(teams))
	for _, team := range teams {
		cards = append(cards, teamCard(c, team, cityNames[team.CityID], viewer))
	}
	utils.RespondSuccess(c, http.StatusOK, cards, "")
}

type createTeamInput struct {
	Name       string `json:"name" binding:"required,min=2,max=60"`
	CityID     string `json:"city_id" binding:"required,uuid"`
	Area       string `json:"area" binding:"max=120"`
	Format     string `json:"format" binding:"required,min=2,max=40"`
	Bio        string `json:"bio" binding:"max=280"`
	BadgeColor string `json:"badge_color" binding:"omitempty,hexcolor"`
	Recruiting bool   `json:"recruiting"`
	Needs      string `json:"needs" binding:"max=120"`
}

func teamTag(name string) string {
	words := strings.Fields(strings.ToUpper(name))
	tag := ""
	for _, word := range words {
		tag += string([]rune(word)[0])
		if len(tag) >= 3 {
			break
		}
	}
	if len(tag) < 2 && len(name) >= 2 {
		tag = strings.ToUpper(name[:2])
	}
	return tag
}

func CreateTeam(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in to create a team")
		return
	}
	var input createTeamInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "name, city_id and format are required")
		return
	}
	cityID, err := uuid.Parse(input.CityID)
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CITY", "city_id is invalid")
		return
	}
	badge := strings.TrimSpace(input.BadgeColor)
	if badge == "" {
		badge = "#0e3b2c"
	}
	team := models.Team{
		Name: strings.TrimSpace(input.Name), Tag: teamTag(input.Name), CaptainID: userID,
		CityID: cityID, Area: strings.TrimSpace(input.Area), Bio: strings.TrimSpace(input.Bio),
		BadgeColor: badge, Format: strings.TrimSpace(input.Format),
		Recruiting: input.Recruiting, Needs: strings.TrimSpace(input.Needs),
	}
	err = initializers.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&team).Error; err != nil {
			return err
		}
		member := models.TeamMember{TeamID: team.ID, UserID: userID, Role: memberRoleCaptain, Status: memberStatusActive, JoinedAt: time.Now().UTC()}
		return tx.Create(&member).Error
	})
	if err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "TEAM_CREATE_FAILED", "could not create the team")
		return
	}
	var city models.City
	initializers.DB.WithContext(c.Request.Context()).First(&city, "id = ?", cityID)
	utils.RespondSuccess(c, http.StatusCreated, teamCard(c, team, city.Name, userID), "Team created — karibu kwenye jamii.")
}

type teamMemberDTO struct {
	UserID uuid.UUID `json:"user_id"`
	Name   string    `json:"name"`
	Avatar string    `json:"avatar_url,omitempty"`
	Role   string    `json:"role"`
	Status string    `json:"status"`
}

// GetTeam returns the team card plus its members; pending join requests are
// visible to the captain only.
func GetTeam(c *gin.Context) {
	viewer := viewerID(c)
	teamID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var team models.Team
	if err := initializers.DB.WithContext(c.Request.Context()).First(&team, "id = ?", teamID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "TEAM_NOT_FOUND", "team was not found")
		return
	}
	var city models.City
	initializers.DB.WithContext(c.Request.Context()).First(&city, "id = ?", team.CityID)

	type memberRow struct {
		models.TeamMember
		Name      string
		AvatarURL string
	}
	var rows []memberRow
	query := initializers.DB.WithContext(c.Request.Context()).
		Table("team_members").
		Select("team_members.*, users.name, users.avatar_url").
		Joins("JOIN users ON users.id = team_members.user_id").
		Where("team_members.team_id = ?", teamID)
	if viewer != team.CaptainID {
		query = query.Where("team_members.status = ?", memberStatusActive)
	}
	query.Order("team_members.joined_at ASC").Scan(&rows)

	members := make([]teamMemberDTO, 0, len(rows))
	for _, row := range rows {
		members = append(members, teamMemberDTO{
			UserID: row.UserID, Name: row.Name, Avatar: row.AvatarURL, Role: row.Role, Status: row.Status,
		})
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{
		"team": teamCard(c, team, city.Name, viewer), "members": members,
	}, "")
}

// RequestJoinTeam: a solo player knocks on the door; the captain gets an email.
func RequestJoinTeam(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in to join a team")
		return
	}
	teamID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var team models.Team
	if err := initializers.DB.WithContext(c.Request.Context()).First(&team, "id = ?", teamID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "TEAM_NOT_FOUND", "team was not found")
		return
	}
	var existing models.TeamMember
	if initializers.DB.WithContext(c.Request.Context()).
		First(&existing, "team_id = ? AND user_id = ?", teamID, userID).Error == nil {
		utils.RespondSuccess(c, http.StatusOK, gin.H{"status": existing.Status}, "You already have a request with this team.")
		return
	}
	member := models.TeamMember{TeamID: teamID, UserID: userID, Role: memberRolePlayer, Status: memberStatusRequested, JoinedAt: time.Now().UTC()}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&member).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "JOIN_FAILED", "could not send the join request")
		return
	}
	var player, captain models.User
	initializers.DB.WithContext(c.Request.Context()).First(&player, "id = ?", userID)
	initializers.DB.WithContext(c.Request.Context()).First(&captain, "id = ?", team.CaptainID)
	if captain.Email != nil {
		go services.SendTeamJoinRequestEmail(*captain.Email, captain.Name, player.Name, team.Name, team.ID)
	}
	go services.SendToUser(team.CaptainID,
		services.Text{EN: "New join request", SW: "Ombi jipya la kujiunga"},
		services.Text{
			EN: player.Name + " wants to join " + team.Name,
			SW: player.Name + " anataka kujiunga na " + team.Name,
		},
		map[string]string{"type": "team_join_request", "team_id": team.ID.String()})
	utils.RespondSuccess(c, http.StatusCreated, gin.H{"status": memberStatusRequested}, "Request sent — the captain will review it.")
}

type decideJoinInput struct {
	UserID string `json:"user_id" binding:"required,uuid"`
	Accept bool   `json:"accept"`
}

// DecideJoinRequest: captain approves or declines; the player gets an email.
func DecideJoinRequest(c *gin.Context) {
	captainID, ok := clientUserID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in first")
		return
	}
	teamID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var team models.Team
	if err := initializers.DB.WithContext(c.Request.Context()).First(&team, "id = ?", teamID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "TEAM_NOT_FOUND", "team was not found")
		return
	}
	if team.CaptainID != captainID {
		utils.RespondError(c, http.StatusForbidden, "NOT_CAPTAIN", "only the captain can decide join requests")
		return
	}
	var input decideJoinInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "user_id and accept are required")
		return
	}
	playerID, _ := uuid.Parse(input.UserID)
	if input.Accept {
		initializers.DB.WithContext(c.Request.Context()).Model(&models.TeamMember{}).
			Where("team_id = ? AND user_id = ?", teamID, playerID).
			Updates(map[string]interface{}{"status": memberStatusActive, "joined_at": time.Now().UTC()})
	} else {
		initializers.DB.WithContext(c.Request.Context()).
			Where("team_id = ? AND user_id = ?", teamID, playerID).Delete(&models.TeamMember{})
	}
	var player models.User
	if initializers.DB.WithContext(c.Request.Context()).First(&player, "id = ?", playerID).Error == nil && player.Email != nil {
		go services.SendTeamDecisionEmail(*player.Email, player.Name, team.Name, input.Accept, team.ID)
	}
	if input.Accept {
		go services.SendToUser(playerID,
			services.Text{EN: "You're in! 🎉", SW: "Umekubaliwa! 🎉"},
			services.Text{
				EN: "You joined " + team.Name,
				SW: "Umejiunga na " + team.Name,
			},
			map[string]string{"type": "team_accepted", "team_id": team.ID.String()})
	}
	utils.RespondSuccess(c, http.StatusOK, gin.H{"accepted": input.Accept}, "Decision saved.")
}

type challengeInput struct {
	Note       string     `json:"note" binding:"max=200"`
	ProposedAt *time.Time `json:"proposed_at"`
}

// CreateChallenge posts an open challenge from the captain's team.
func CreateChallenge(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in first")
		return
	}
	teamID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var team models.Team
	if err := initializers.DB.WithContext(c.Request.Context()).First(&team, "id = ?", teamID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "TEAM_NOT_FOUND", "team was not found")
		return
	}
	if team.CaptainID != userID {
		utils.RespondError(c, http.StatusForbidden, "NOT_CAPTAIN", "only the captain can post a challenge")
		return
	}
	var input challengeInput
	_ = c.ShouldBindJSON(&input)
	challenge := models.Challenge{
		TeamID: teamID, CityID: team.CityID, Format: team.Format,
		Note: strings.TrimSpace(input.Note), ProposedAt: input.ProposedAt, Status: "open",
	}
	if err := initializers.DB.WithContext(c.Request.Context()).Create(&challenge).Error; err != nil {
		utils.RespondError(c, http.StatusInternalServerError, "CHALLENGE_FAILED", "could not post the challenge")
		return
	}
	utils.RespondSuccess(c, http.StatusCreated, gin.H{"id": challenge.ID}, "Challenge posted — game on.")
}

type challengeDTO struct {
	ID         uuid.UUID  `json:"id"`
	TeamID     uuid.UUID  `json:"team_id"`
	TeamName   string     `json:"team_name"`
	TeamTag    string     `json:"team_tag"`
	BadgeColor string     `json:"badge_color"`
	City       string     `json:"city"`
	Area       string     `json:"area"`
	Format     string     `json:"format"`
	Note       string     `json:"note"`
	ProposedAt *time.Time `json:"proposed_at,omitempty"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ListChallenges is the public open-challenges board.
func ListChallenges(c *gin.Context) {
	type row struct {
		models.Challenge
		TeamName   string
		TeamTag    string
		BadgeColor string
		Area       string
		CityName   string
	}
	query := initializers.DB.WithContext(c.Request.Context()).
		Table("challenges").
		Select("challenges.*, teams.name AS team_name, teams.tag AS team_tag, teams.badge_color, teams.area, cities.name AS city_name").
		Joins("JOIN teams ON teams.id = challenges.team_id").
		Joins("JOIN cities ON cities.id = challenges.city_id").
		Where("challenges.status = 'open'")
	if cityID := strings.TrimSpace(c.Query("city_id")); cityID != "" {
		query = query.Where("challenges.city_id = ?", cityID)
	}
	var rows []row
	query.Order("challenges.created_at DESC").Limit(40).Scan(&rows)
	items := make([]challengeDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, challengeDTO{
			ID: r.ID, TeamID: r.TeamID, TeamName: r.TeamName, TeamTag: r.TeamTag,
			BadgeColor: r.BadgeColor, City: r.CityName, Area: r.Area, Format: r.Format,
			Note: r.Note, ProposedAt: r.ProposedAt, Status: r.Status, CreatedAt: r.CreatedAt,
		})
	}
	utils.RespondSuccess(c, http.StatusOK, items, "")
}

type acceptChallengeInput struct {
	TeamID string `json:"team_id" binding:"required,uuid"`
}

// AcceptChallenge: another captain takes the game — a Match is created and
// both captains get an email to arrange the booking.
func AcceptChallenge(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in first")
		return
	}
	challengeID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input acceptChallengeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_INPUT", "team_id is required")
		return
	}
	accepterTeamID, _ := uuid.Parse(input.TeamID)
	var accepterTeam models.Team
	if err := initializers.DB.WithContext(c.Request.Context()).First(&accepterTeam, "id = ?", accepterTeamID).Error; err != nil {
		utils.RespondError(c, http.StatusNotFound, "TEAM_NOT_FOUND", "your team was not found")
		return
	}
	if accepterTeam.CaptainID != userID {
		utils.RespondError(c, http.StatusForbidden, "NOT_CAPTAIN", "only your team's captain can accept a challenge")
		return
	}

	var challenge models.Challenge
	var match models.Match
	// Race-safe accept: the conditional update wins or loses atomically.
	result := initializers.DB.WithContext(c.Request.Context()).Model(&models.Challenge{}).
		Where("id = ? AND status = 'open'", challengeID).
		Updates(map[string]interface{}{"status": "accepted", "accepted_by_team_id": accepterTeamID})
	if result.Error != nil || result.RowsAffected == 0 {
		utils.RespondError(c, http.StatusConflict, "CHALLENGE_TAKEN", "this challenge is no longer open")
		return
	}
	initializers.DB.WithContext(c.Request.Context()).First(&challenge, "id = ?", challengeID)
	if challenge.TeamID == accepterTeamID {
		initializers.DB.WithContext(c.Request.Context()).Model(&models.Challenge{}).
			Where("id = ?", challengeID).Updates(map[string]interface{}{"status": "open", "accepted_by_team_id": nil})
		utils.RespondError(c, http.StatusBadRequest, "OWN_CHALLENGE", "you cannot accept your own challenge")
		return
	}
	match = models.Match{HomeTeamID: challenge.TeamID, AwayTeamID: accepterTeamID, Status: "challenge_accepted"}
	if initializers.DB.WithContext(c.Request.Context()).Create(&match).Error == nil {
		initializers.DB.WithContext(c.Request.Context()).Model(&models.Challenge{}).
			Where("id = ?", challengeID).Update("match_id", match.ID)
	}

	var homeTeam models.Team
	initializers.DB.WithContext(c.Request.Context()).First(&homeTeam, "id = ?", challenge.TeamID)
	var homeCaptain, awayCaptain models.User
	initializers.DB.WithContext(c.Request.Context()).First(&homeCaptain, "id = ?", homeTeam.CaptainID)
	initializers.DB.WithContext(c.Request.Context()).First(&awayCaptain, "id = ?", accepterTeam.CaptainID)
	if homeCaptain.Email != nil {
		go services.SendChallengeAcceptedEmail(*homeCaptain.Email, homeCaptain.Name, homeTeam.Name, accepterTeam.Name, match.ID)
	}
	if awayCaptain.Email != nil {
		go services.SendChallengeAcceptedEmail(*awayCaptain.Email, awayCaptain.Name, accepterTeam.Name, homeTeam.Name, match.ID)
	}
	go services.SendToUsers([]uuid.UUID{homeTeam.CaptainID, accepterTeam.CaptainID},
		services.Text{EN: "Challenge accepted 🔥", SW: "Changamoto imekubaliwa 🔥"},
		services.Text{
			EN: homeTeam.Name + " vs " + accepterTeam.Name + " is on — agree a time.",
			SW: homeTeam.Name + " dhidi ya " + accepterTeam.Name + " imepangwa — kubalianeni muda.",
		},
		map[string]string{"type": "challenge_accepted", "match_id": match.ID.String()})
	utils.RespondSuccess(c, http.StatusOK, gin.H{"match_id": match.ID}, "Challenge accepted — arrange the game!")
}

// MyTeams lists the signed-in player's teams and pending requests.
func MyTeams(c *gin.Context) {
	userID, ok := clientUserID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in first")
		return
	}
	var memberships []models.TeamMember
	initializers.DB.WithContext(c.Request.Context()).
		Where("user_id = ?", userID).Find(&memberships)
	cityNames := map[uuid.UUID]string{}
	var cities []models.City
	initializers.DB.WithContext(c.Request.Context()).Find(&cities)
	for _, city := range cities {
		cityNames[city.ID] = city.Name
	}
	cards := make([]teamCardDTO, 0, len(memberships))
	for _, membership := range memberships {
		var team models.Team
		if initializers.DB.WithContext(c.Request.Context()).First(&team, "id = ?", membership.TeamID).Error == nil {
			cards = append(cards, teamCard(c, team, cityNames[team.CityID], userID))
		}
	}
	utils.RespondSuccess(c, http.StatusOK, cards, "")
}
