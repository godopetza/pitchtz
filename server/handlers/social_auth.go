package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/initializers"
	"github.com/godopetza/pitchtz/models"
	"github.com/godopetza/pitchtz/utils"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type socialState struct {
	Audience string `json:"aud"`
	Provider string `json:"provider"`
	Nonce    string `json:"nonce"`
	Expires  int64  `json:"exp"`
}

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type googleUserInfo struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// appleUserPayload is Apple's optional "user" form field, present ONLY on the
// very first authorization for a given app — Apple never sends a picture.
type appleUserPayload struct {
	Name struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"name"`
}

type appleKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

var appleKeysCache struct {
	sync.Mutex
	keys      map[string]*rsa.PublicKey
	expiresAt time.Time
}

func AdminGoogleStart(c *gin.Context) { startGoogle(c, models.PasswordResetAudienceAdmin) }
func OwnerGoogleStart(c *gin.Context) { startGoogle(c, models.PasswordResetAudienceOwner) }
func AdminAppleStart(c *gin.Context)  { startApple(c, models.PasswordResetAudienceAdmin) }
func OwnerAppleStart(c *gin.Context)  { startApple(c, models.PasswordResetAudienceOwner) }

func startGoogle(c *gin.Context, audience string) {
	clientID := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	if clientID == "" {
		redirectSocialError(c, audience, "provider_not_configured")
		return
	}
	state, err := issueSocialState(audience, "google")
	if err != nil {
		redirectSocialError(c, audience, "provider_not_configured")
		return
	}
	query := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {publicAPIURL(c) + "/v1/auth/google/callback"},
		"response_type": {"code"},
		"scope":         {"openid email profile"},
		"prompt":        {"select_account"},
		"state":         {state},
	}
	c.Redirect(http.StatusFound, "https://accounts.google.com/o/oauth2/v2/auth?"+query.Encode())
}

func GoogleCallback(c *gin.Context) {
	state, err := verifySocialState(c.Query("state"), "google")
	if err != nil || c.Query("error") != "" {
		redirectSocialError(c, state.Audience, "social_sign_in_failed")
		return
	}
	clientID := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET"))
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {c.Query("code")},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {publicAPIURL(c) + "/v1/auth/google/callback"},
	}
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		if response != nil {
			response.Body.Close()
		}
		redirectSocialError(c, state.Audience, "social_sign_in_failed")
		return
	}
	defer response.Body.Close()
	var token googleTokenResponse
	if json.NewDecoder(response.Body).Decode(&token) != nil || token.AccessToken == "" {
		redirectSocialError(c, state.Audience, "social_sign_in_failed")
		return
	}

	infoReq, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	infoReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	infoResponse, err := (&http.Client{Timeout: 12 * time.Second}).Do(infoReq)
	if err != nil || infoResponse.StatusCode != http.StatusOK {
		if infoResponse != nil {
			infoResponse.Body.Close()
		}
		redirectSocialError(c, state.Audience, "social_sign_in_failed")
		return
	}
	defer infoResponse.Body.Close()
	var profile googleUserInfo
	if json.NewDecoder(infoResponse.Body).Decode(&profile) != nil || !profile.EmailVerified {
		redirectSocialError(c, state.Audience, "email_not_verified")
		return
	}
	finishExistingSocialLogin(c, state.Audience, profile.Email, "google", profile.Name, profile.Picture)
}

func startApple(c *gin.Context, audience string) {
	clientID := strings.TrimSpace(os.Getenv("APPLE_CLIENT_ID"))
	if clientID == "" {
		redirectSocialError(c, audience, "provider_not_configured")
		return
	}
	state, err := issueSocialState(audience, "apple")
	if err != nil {
		redirectSocialError(c, audience, "provider_not_configured")
		return
	}
	verified, _ := verifySocialState(state, "apple")
	query := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {publicAPIURL(c) + "/v1/auth/apple/callback"},
		"response_type": {"code id_token"},
		"response_mode": {"form_post"},
		"scope":         {"name email"},
		"state":         {state},
		"nonce":         {verified.Nonce},
	}
	c.Redirect(http.StatusFound, "https://appleid.apple.com/auth/authorize?"+query.Encode())
}

func AppleCallback(c *gin.Context) {
	state, err := verifySocialState(c.PostForm("state"), "apple")
	if err != nil || c.PostForm("error") != "" {
		redirectSocialError(c, state.Audience, "social_sign_in_failed")
		return
	}
	email, nonce, err := verifyAppleIdentityToken(c.Request.Context(), c.PostForm("id_token"))
	if err != nil || !hmac.Equal([]byte(nonce), []byte(state.Nonce)) {
		redirectSocialError(c, state.Audience, "social_sign_in_failed")
		return
	}
	name := ""
	if raw := c.PostForm("user"); raw != "" {
		var payload appleUserPayload
		if json.Unmarshal([]byte(raw), &payload) == nil {
			name = strings.TrimSpace(payload.Name.FirstName + " " + payload.Name.LastName)
		}
	}
	finishExistingSocialLogin(c, state.Audience, email, "apple", name, "")
}

func finishExistingSocialLogin(c *gin.Context, audience, email, provider, name, avatarURL string) {
	if initializers.DB == nil {
		redirectSocialError(c, audience, "database_unavailable")
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		redirectSocialError(c, audience, "email_not_verified")
		return
	}

	if audience == models.PasswordResetAudienceClient {
		finishClientSocialLogin(c, email, provider, name, avatarURL)
		return
	}

	var user models.User
	userMissing := initializers.DB.WithContext(c.Request.Context()).Where("LOWER(email) = ?", email).First(&user).Error != nil

	var token string
	var err error
	if audience == models.PasswordResetAudienceAdmin {
		// Admin stays invite-only: an unknown Google/Apple identity gets no
		// account here.
		if userMissing || user.Email == nil {
			redirectSocialError(c, audience, "account_not_provisioned")
			return
		}
		var staff models.AdminStaff
		if initializers.DB.WithContext(c.Request.Context()).First(&staff, "user_id = ? AND status = ?", user.ID, models.AdminStatusActive).Error != nil {
			redirectSocialError(c, audience, "account_not_provisioned")
			return
		}
		token, _, err = utils.IssueAdminToken(user.ID, staff.Role)
	} else {
		// Owner is open onboarding: Google/Apple verified the email, so let
		// them in and collect venue details inside. The real marketplace gate
		// stays venue approval, not account creation.
		now := time.Now().UTC()
		if userMissing {
			displayName := strings.TrimSpace(name)
			if displayName == "" {
				displayName = strings.Split(email, "@")[0]
			}
			user = models.User{Email: &email, Name: displayName, AvatarURL: avatarURL, AuthProvider: provider, Language: "sw", Role: "owner", EmailVerifiedAt: &now}
			if createErr := initializers.DB.WithContext(c.Request.Context()).Create(&user).Error; createErr != nil {
				if refetchErr := initializers.DB.WithContext(c.Request.Context()).Where("LOWER(email) = ?", email).First(&user).Error; refetchErr != nil {
					redirectSocialError(c, audience, "account_lookup_failed")
					return
				}
			}
		}
		var credential models.OwnerCredential
		if initializers.DB.WithContext(c.Request.Context()).First(&credential, "user_id = ?", user.ID).Error != nil {
			// First social sign-in (or an enrolled-but-unapproved owner):
			// mint a credential with an unusable password — social is their way in.
			randomSecret := uuid.NewString() + uuid.NewString()
			hash, hashErr := bcrypt.GenerateFromPassword([]byte(randomSecret), bcrypt.DefaultCost)
			if hashErr != nil {
				redirectSocialError(c, audience, "auth_not_configured")
				return
			}
			credential = models.OwnerCredential{UserID: user.ID, Status: models.OwnerStatusActive, PasswordHash: string(hash), MustChangePassword: false}
			if createErr := initializers.DB.WithContext(c.Request.Context()).Create(&credential).Error; createErr != nil {
				initializers.DB.WithContext(c.Request.Context()).First(&credential, "user_id = ?", user.ID)
			}
		}
		if credential.Status != models.OwnerStatusActive {
			redirectSocialError(c, audience, "account_not_provisioned")
			return
		}
		token, _, err = utils.IssueOwnerToken(user.ID)
	}
	if err != nil {
		redirectSocialError(c, audience, "auth_not_configured")
		return
	}
	now := time.Now().UTC()
	updates := map[string]interface{}{"last_login_provider": provider, "last_login_at": now}
	if strings.TrimSpace(name) != "" {
		updates["name"] = strings.TrimSpace(name)
	}
	if strings.TrimSpace(avatarURL) != "" {
		updates["avatar_url"] = strings.TrimSpace(avatarURL)
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.User{}).Where("id = ?", user.ID).Updates(updates)
	if audience == models.PasswordResetAudienceAdmin {
		initializers.DB.WithContext(c.Request.Context()).Model(&models.AdminStaff{}).Where("user_id = ?", user.ID).Update("last_active_at", now)
	}
	c.Redirect(http.StatusFound, portalURL(audience)+"/#oauth_token="+url.QueryEscape(token))
}

// finishClientSocialLogin is the customer-facing counterpart: unlike
// owner/admin, there is no pre-provisioning step. Google/Apple already
// verified the email, so a first-time sign-in just creates the account.
func finishClientSocialLogin(c *gin.Context, email, provider, name, avatarURL string) {
	var user models.User
	err := initializers.DB.WithContext(c.Request.Context()).Where("LOWER(email) = ?", email).First(&user).Error
	now := time.Now().UTC()
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}
	if err != nil {
		user = models.User{Email: &email, Name: displayName, AvatarURL: avatarURL, AuthProvider: provider, Language: "sw", Role: "player", EmailVerifiedAt: &now}
		if createErr := initializers.DB.WithContext(c.Request.Context()).Create(&user).Error; createErr != nil {
			if refetchErr := initializers.DB.WithContext(c.Request.Context()).Where("LOWER(email) = ?", email).First(&user).Error; refetchErr != nil {
				redirectSocialError(c, models.PasswordResetAudienceClient, "account_lookup_failed")
				return
			}
		}
	}
	token, _, err := utils.IssueClientToken(user.ID)
	if err != nil {
		redirectSocialError(c, models.PasswordResetAudienceClient, "auth_not_configured")
		return
	}
	updates := map[string]interface{}{"last_login_provider": provider, "last_login_at": now}
	if user.EmailVerifiedAt == nil {
		updates["email_verified_at"] = now
	}
	// Apple only sends a name on the very first authorization, and never sends
	// a picture — never overwrite a known-good name/avatar with a blank one.
	if strings.TrimSpace(name) != "" {
		updates["name"] = strings.TrimSpace(name)
	}
	if strings.TrimSpace(avatarURL) != "" {
		updates["avatar_url"] = strings.TrimSpace(avatarURL)
	}
	initializers.DB.WithContext(c.Request.Context()).Model(&models.User{}).Where("id = ?", user.ID).Updates(updates)
	c.Redirect(http.StatusFound, portalURL(models.PasswordResetAudienceClient)+"/#oauth_token="+url.QueryEscape(token))
}

func issueSocialState(audience, provider string) (string, error) {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if len(secret) < 32 {
		return "", errors.New("JWT_SECRET must be configured")
	}
	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	payload, err := json.Marshal(socialState{Audience: audience, Provider: provider, Nonce: base64.RawURLEncoding.EncodeToString(nonceBytes), Expires: time.Now().Add(10 * time.Minute).Unix()})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func verifySocialState(value, provider string) (socialState, error) {
	var state socialState
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return state, errors.New("invalid state")
	}
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return state, err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return state, errors.New("invalid state signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(payload, &state) != nil || state.Provider != provider || state.Expires < time.Now().Unix() {
		return socialState{}, errors.New("invalid or expired state")
	}
	if state.Audience != models.PasswordResetAudienceAdmin && state.Audience != models.PasswordResetAudienceOwner && state.Audience != models.PasswordResetAudienceClient {
		return socialState{}, errors.New("invalid audience")
	}
	return state, nil
}

func verifyAppleIdentityToken(ctx context.Context, raw string) (string, string, error) {
	clientID := strings.TrimSpace(os.Getenv("APPLE_CLIENT_ID"))
	parsed, err := jwt.Parse(raw, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != "RS256" {
			return nil, fmt.Errorf("unexpected signing method %s", token.Method.Alg())
		}
		kid, _ := token.Header["kid"].(string)
		keys, err := applePublicKeys(ctx)
		if err != nil {
			return nil, err
		}
		key := keys[kid]
		if key == nil {
			return nil, errors.New("apple signing key not found")
		}
		return key, nil
	}, jwt.WithAudience(clientID), jwt.WithIssuer("https://appleid.apple.com"), jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !parsed.Valid {
		return "", "", errors.New("invalid apple identity token")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", errors.New("invalid apple claims")
	}
	email, _ := claims["email"].(string)
	nonce, _ := claims["nonce"].(string)
	if email == "" || nonce == "" {
		return "", "", errors.New("apple email or nonce missing")
	}
	return email, nonce, nil
}

func applePublicKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	appleKeysCache.Lock()
	defer appleKeysCache.Unlock()
	if time.Now().Before(appleKeysCache.expiresAt) && len(appleKeysCache.keys) > 0 {
		return appleKeysCache.keys, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://appleid.apple.com/auth/keys", nil)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apple keys returned %d", response.StatusCode)
	}
	var payload struct {
		Keys []appleKey `json:"keys"`
	}
	if json.NewDecoder(response.Body).Decode(&payload) != nil {
		return nil, errors.New("invalid apple keys response")
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, item := range payload.Keys {
		nBytes, errN := base64.RawURLEncoding.DecodeString(item.N)
		eBytes, errE := base64.RawURLEncoding.DecodeString(item.E)
		if errN != nil || errE != nil || len(eBytes) == 0 {
			continue
		}
		exponent := 0
		for _, b := range eBytes {
			exponent = exponent<<8 + int(b)
		}
		keys[item.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: exponent}
	}
	appleKeysCache.keys = keys
	appleKeysCache.expiresAt = time.Now().Add(6 * time.Hour)
	return keys, nil
}

func publicAPIURL(c *gin.Context) string {
	if configured := strings.TrimRight(strings.TrimSpace(os.Getenv("API_PUBLIC_URL")), "/"); configured != "" {
		return configured
	}
	proto := c.GetHeader("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
	}
	return proto + "://" + c.Request.Host
}

func portalURL(audience string) string {
	key := "OWNER_APP_URL"
	fallback := "http://localhost:3002"
	switch audience {
	case models.PasswordResetAudienceAdmin:
		key = "ADMIN_APP_URL"
		fallback = "http://localhost:3001"
	case models.PasswordResetAudienceClient:
		key = "CLIENT_APP_URL"
		fallback = "http://localhost:3000"
	}
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); value != "" {
		return value
	}
	return fallback
}

func redirectSocialError(c *gin.Context, audience, code string) {
	if audience != models.PasswordResetAudienceAdmin && audience != models.PasswordResetAudienceOwner && audience != models.PasswordResetAudienceClient {
		audience = models.PasswordResetAudienceOwner
	}
	c.Redirect(http.StatusFound, portalURL(audience)+"/?oauth_error="+url.QueryEscape(code)+"&t="+strconv.FormatInt(time.Now().Unix(), 10))
}
