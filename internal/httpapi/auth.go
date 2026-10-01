package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/simpul/hr-backend/internal/domain"
	"github.com/simpul/hr-backend/internal/platform/secure"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

type loginRequest struct {
	Email            string `json:"email" binding:"required,email"`
	Password         string `json:"password" binding:"required,min=8,max=128"`
	OrganizationSlug string `json:"organizationSlug"`
}

type mfaRequest struct {
	ChallengeToken string `json:"challengeToken" binding:"required"`
	Code           string `json:"code" binding:"required"`
}

func (s *Server) login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Data login tidak valid", err.Error(), nil)
		return
	}
	var user domain.User
	query := s.db.Preload("Memberships.Organization").Preload("Memberships.Role.Permissions").Where("lower(email) = ?", strings.ToLower(strings.TrimSpace(request.Email)))
	if err := query.First(&user).Error; err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)) != nil {
		problem(c, http.StatusUnauthorized, "invalid_credentials", "Login gagal", "Email atau kata sandi salah.", nil)
		return
	}
	if len(user.Memberships) == 0 {
		problem(c, http.StatusForbidden, "no_membership", "Workspace tidak tersedia", "Akun belum terhubung dengan organisasi.", nil)
		return
	}
	var membership domain.Membership
	if request.OrganizationSlug != "" {
		for _, candidate := range user.Memberships {
			if candidate.Organization.Slug == request.OrganizationSlug {
				membership = candidate
				break
			}
		}
		if membership.ID == "" {
			problem(c, http.StatusForbidden, "workspace_denied", "Workspace tidak tersedia", "Akun tidak mempunyai akses ke workspace tersebut.", nil)
			return
		}
	} else if len(user.Memberships) > 1 {
		workspaces := make([]gin.H, 0, len(user.Memberships))
		for _, item := range user.Memberships {
			workspaces = append(workspaces, gin.H{"slug": item.Organization.Slug, "name": item.Organization.Name})
		}
		c.JSON(http.StatusOK, gin.H{"organizationSelectionRequired": true, "organizations": workspaces})
		return
	} else {
		membership = user.Memberships[0]
	}

	if s.config.MFARequiredRoles[membership.Role.Key] {
		challenge, _ := s.tokens.Issue(secure.Claims{UserID: user.ID, OrganizationID: membership.OrganizationID, MembershipID: membership.ID, Role: membership.Role.Key, TokenType: "mfa"}, 5*time.Minute)
		if !user.MFAEnabled {
			c.JSON(http.StatusAccepted, gin.H{"mfaEnrollmentRequired": true, "challengeToken": challenge})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"mfaRequired": true, "challengeToken": challenge})
		return
	}
	s.issueSession(c, user, membership)
}

func (s *Server) mfaEnroll(c *gin.Context) {
	var request struct {
		ChallengeToken string `json:"challengeToken" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Data tidak valid", err.Error(), nil)
		return
	}
	claims, err := s.tokens.Parse(request.ChallengeToken, "mfa")
	if err != nil {
		problem(c, http.StatusUnauthorized, "invalid_challenge", "Tantangan tidak valid", "Ulangi proses login.", nil)
		return
	}
	var user domain.User
	if err := s.db.First(&user, "id = ?", claims.UserID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Simpul HR", AccountName: user.Email})
	if err != nil {
		problem(c, http.StatusInternalServerError, "mfa_setup_failed", "MFA gagal dibuat", err.Error(), nil)
		return
	}
	encrypted, err := s.cipher.Encrypt(key.Secret())
	if err != nil {
		problem(c, http.StatusInternalServerError, "encryption_failed", "MFA gagal dibuat", err.Error(), nil)
		return
	}
	if err := s.db.Model(&user).Updates(map[string]any{"mfa_secret_enc": encrypted, "mfa_enabled": false}).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"secret": key.Secret(), "otpauthUri": key.URL()})
}

func (s *Server) mfaConfirm(c *gin.Context) {
	var request mfaRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Data tidak valid", err.Error(), nil)
		return
	}
	claims, err := s.tokens.Parse(request.ChallengeToken, "mfa")
	if err != nil {
		problem(c, http.StatusUnauthorized, "invalid_challenge", "Tantangan tidak valid", "Ulangi proses login.", nil)
		return
	}
	user, membership, secret, ok := s.mfaIdentity(c, claims)
	if !ok {
		return
	}
	if !totp.Validate(strings.TrimSpace(request.Code), secret) {
		problem(c, http.StatusUnauthorized, "invalid_mfa_code", "Kode tidak valid", "Periksa waktu perangkat dan coba lagi.", nil)
		return
	}
	recoveryCodes := make([]string, 0, 8)
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&user).Update("mfa_enabled", true).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&domain.MFARecoveryCode{}).Error; err != nil {
			return err
		}
		for range 8 {
			plain, hash, err := secure.RandomToken(9)
			if err != nil {
				return err
			}
			recoveryCodes = append(recoveryCodes, plain)
			if err := tx.Create(&domain.MFARecoveryCode{UserID: user.ID, CodeHash: hash}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		databaseProblem(c, err)
		return
	}
	s.issueSessionWithExtra(c, user, membership, gin.H{"recoveryCodes": recoveryCodes})
}

func (s *Server) mfaVerify(c *gin.Context) {
	var request mfaRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Data tidak valid", err.Error(), nil)
		return
	}
	claims, err := s.tokens.Parse(request.ChallengeToken, "mfa")
	if err != nil {
		problem(c, http.StatusUnauthorized, "invalid_challenge", "Tantangan tidak valid", "Ulangi proses login.", nil)
		return
	}
	user, membership, secret, ok := s.mfaIdentity(c, claims)
	if !ok {
		return
	}
	valid := totp.Validate(strings.TrimSpace(request.Code), secret)
	if !valid {
		hash := secure.HashToken(strings.TrimSpace(request.Code))
		var recovery domain.MFARecoveryCode
		if err := s.db.Where("user_id = ? AND code_hash = ? AND used_at IS NULL", user.ID, hash).First(&recovery).Error; err == nil {
			now := time.Now().UTC()
			valid = s.db.Model(&recovery).Update("used_at", now).Error == nil
		}
	}
	if !valid {
		problem(c, http.StatusUnauthorized, "invalid_mfa_code", "Kode tidak valid", "Gunakan kode authenticator atau recovery code yang belum dipakai.", nil)
		return
	}
	s.issueSession(c, user, membership)
}

func (s *Server) mfaIdentity(c *gin.Context, claims secure.Claims) (domain.User, domain.Membership, string, bool) {
	var user domain.User
	if err := s.db.First(&user, "id = ?", claims.UserID).Error; err != nil {
		databaseProblem(c, err)
		return user, domain.Membership{}, "", false
	}
	var membership domain.Membership
	if err := s.db.Preload("Role.Permissions").Preload("Organization").Where("id = ? AND user_id = ? AND organization_id = ?", claims.MembershipID, claims.UserID, claims.OrganizationID).First(&membership).Error; err != nil {
		databaseProblem(c, err)
		return user, membership, "", false
	}
	secret, err := s.cipher.Decrypt(user.MFASecretEnc)
	if err != nil || secret == "" {
		problem(c, http.StatusConflict, "mfa_not_enrolled", "MFA belum disiapkan", "Mulai ulang proses enrollment.", nil)
		return user, membership, "", false
	}
	return user, membership, secret, true
}

func (s *Server) issueSession(c *gin.Context, user domain.User, membership domain.Membership) {
	s.issueSessionWithExtra(c, user, membership, nil)
}

func (s *Server) issueSessionWithExtra(c *gin.Context, user domain.User, membership domain.Membership, extra gin.H) {
	access, permissions, err := s.createSession(c, user, membership)
	if err != nil {
		databaseProblem(c, err)
		return
	}
	response := gin.H{"accessToken": access, "expiresIn": int(s.config.AccessTokenTTL.Seconds()), "user": gin.H{"id": user.ID, "email": user.Email, "role": membership.Role.Key, "organization": membership.Organization, "employeeId": membership.EmployeeID, "permissions": permissions}}
	for key, value := range extra {
		response[key] = value
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) createSession(c *gin.Context, user domain.User, membership domain.Membership) (string, []string, error) {
	permissions := make([]string, 0, len(membership.Role.Permissions))
	for _, permission := range membership.Role.Permissions {
		permissions = append(permissions, permission.Key)
	}
	access, err := s.tokens.Issue(secure.Claims{UserID: user.ID, OrganizationID: membership.OrganizationID, MembershipID: membership.ID, Role: membership.Role.Key, Permissions: permissions, TokenType: "access"}, 0)
	if err != nil {
		return "", nil, fmt.Errorf("issue access token: %w", err)
	}
	plain, hash, err := secure.RandomToken(48)
	if err != nil {
		return "", nil, fmt.Errorf("issue refresh token: %w", err)
	}
	session := domain.RefreshSession{UserID: user.ID, MembershipID: membership.ID, OrganizationID: membership.OrganizationID, TokenHash: hash, ExpiresAt: time.Now().UTC().Add(s.config.RefreshTokenTTL), UserAgent: c.Request.UserAgent(), IPAddress: c.ClientIP()}
	if err := s.db.Create(&session).Error; err != nil {
		return "", nil, fmt.Errorf("persist refresh session: %w", err)
	}
	now := time.Now().UTC()
	_ = s.db.Model(&user).Update("last_login_at", now).Error
	s.setRefreshCookie(c, plain, s.config.RefreshTokenTTL)
	return access, permissions, nil
}

func (s *Server) refresh(c *gin.Context) {
	plain, err := c.Cookie("simpul_refresh")
	if err != nil {
		problem(c, http.StatusUnauthorized, "refresh_required", "Sesi berakhir", "Silakan masuk kembali.", nil)
		return
	}
	hash := secure.HashToken(plain)
	var session domain.RefreshSession
	if err := s.db.Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, time.Now().UTC()).First(&session).Error; err != nil {
		s.clearRefreshCookie(c)
		problem(c, http.StatusUnauthorized, "invalid_refresh", "Sesi berakhir", "Silakan masuk kembali.", nil)
		return
	}
	var user domain.User
	var membership domain.Membership
	if err := s.db.First(&user, "id = ?", session.UserID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if err := s.db.Preload("Role.Permissions").Preload("Organization").First(&membership, "id = ?", session.MembershipID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	now := time.Now().UTC()
	_ = s.db.Model(&session).Update("revoked_at", now).Error
	s.issueSession(c, user, membership)
}

func (s *Server) logout(c *gin.Context) {
	if plain, err := c.Cookie("simpul_refresh"); err == nil {
		now := time.Now().UTC()
		_ = s.db.Model(&domain.RefreshSession{}).Where("token_hash = ?", secure.HashToken(plain)).Update("revoked_at", now).Error
	}
	s.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

func (s *Server) setRefreshCookie(c *gin.Context, token string, ttl time.Duration) {
	c.SetSameSite(s.config.CookieSameSite)
	c.SetCookie("simpul_refresh", token, int(ttl.Seconds()), "/v1/auth", "", s.config.CookieSecure, true)
}
func (s *Server) clearRefreshCookie(c *gin.Context) {
	// Must match the attributes used when setting it, or the browser treats this as a
	// different cookie and the original stays behind.
	c.SetSameSite(s.config.CookieSameSite)
	c.SetCookie("simpul_refresh", "", -1, "/v1/auth", "", s.config.CookieSecure, true)
}

func (s *Server) ssoStart(c *gin.Context) {
	provider := strings.ToLower(c.Param("provider"))
	if !providerConfigured(s.config, provider) {
		problem(c, http.StatusConflict, "provider_not_configured", "SSO belum dikonfigurasi", "Administrator harus mengisi credential "+provider+".", nil)
		return
	}
	organizationID := ""
	if slug := strings.TrimSpace(c.Query("organizationSlug")); slug != "" {
		var organization domain.Organization
		if err := s.db.Select("id").Where("slug = ?", slug).First(&organization).Error; err != nil {
			problem(c, http.StatusNotFound, "workspace_not_found", "Workspace tidak ditemukan", "Periksa slug workspace dan coba lagi.", nil)
			return
		}
		organizationID = organization.ID
	}
	state, err := s.tokens.Issue(secure.Claims{OrganizationID: organizationID, TokenType: "sso_" + provider}, 5*time.Minute)
	if err != nil {
		problem(c, http.StatusInternalServerError, "sso_state_failed", "SSO gagal dimulai", err.Error(), nil)
		return
	}
	config := s.oauthConfig(provider)
	c.JSON(http.StatusOK, gin.H{"provider": provider, "authorizationUrl": config.AuthCodeURL(state, oauth2.AccessTypeOnline)})
}
func (s *Server) ssoCallback(c *gin.Context) {
	provider := strings.ToLower(c.Param("provider"))
	if !providerConfigured(s.config, provider) {
		s.redirectSSO(c, "provider_not_configured")
		return
	}
	claims, err := s.tokens.Parse(c.Query("state"), "sso_"+provider)
	if err != nil || c.Query("code") == "" {
		s.redirectSSO(c, "invalid_state")
		return
	}
	token, err := s.oauthConfig(provider).Exchange(c.Request.Context(), c.Query("code"))
	if err != nil {
		s.logger.Warn("sso exchange failed", "provider", provider, "error", err)
		s.redirectSSO(c, "exchange_failed")
		return
	}
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, s.ssoUserInfoURL(provider), nil)
	if err != nil {
		s.redirectSSO(c, "profile_failed")
		return
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		s.redirectSSO(c, "profile_failed")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		s.redirectSSO(c, "profile_failed")
		return
	}
	var profile struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		EmailVerified     *bool  `json:"email_verified"`
	}
	if err := json.NewDecoder(response.Body).Decode(&profile); err != nil {
		s.redirectSSO(c, "profile_failed")
		return
	}
	email := strings.ToLower(strings.TrimSpace(profile.Email))
	if email == "" {
		email = strings.ToLower(strings.TrimSpace(profile.PreferredUsername))
	}
	if email == "" || (profile.EmailVerified != nil && !*profile.EmailVerified) {
		s.redirectSSO(c, "email_unverified")
		return
	}
	var user domain.User
	err = s.db.Preload("Memberships.Organization").Preload("Memberships.Role.Permissions").Where("lower(email) = ?", email).First(&user).Error
	if err != nil {
		s.redirectSSO(c, "account_not_found")
		return
	}
	membership, err := selectSSOMembership(user.Memberships, claims.OrganizationID)
	if err != nil {
		s.redirectSSO(c, err.Error())
		return
	}
	if _, _, err := s.createSession(c, user, membership); err != nil {
		s.logger.Error("create sso session", "error", err)
		s.redirectSSO(c, "session_failed")
		return
	}
	c.Redirect(http.StatusFound, s.config.FrontendURL+"/login?sso=success")
}

func (s *Server) oauthConfig(provider string) *oauth2.Config {
	callback := s.config.PublicAPIURL + "/v1/auth/sso/" + provider + "/callback"
	switch provider {
	case "entra":
		tenant := url.PathEscape(s.config.EntraTenantID)
		return &oauth2.Config{ClientID: s.config.EntraClientID, ClientSecret: s.config.EntraClientSecret, RedirectURL: callback, Scopes: []string{"openid", "profile", "email"}, Endpoint: oauth2.Endpoint{AuthURL: "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/authorize", TokenURL: "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/token"}}
	default:
		return &oauth2.Config{ClientID: s.config.GoogleClientID, ClientSecret: s.config.GoogleClientSecret, RedirectURL: callback, Scopes: []string{"openid", "profile", "email"}, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token"}}
	}
}

func (s *Server) ssoUserInfoURL(provider string) string {
	if provider == "entra" {
		return "https://graph.microsoft.com/oidc/userinfo"
	}
	return "https://openidconnect.googleapis.com/v1/userinfo"
}

func (s *Server) redirectSSO(c *gin.Context, code string) {
	c.Redirect(http.StatusFound, s.config.FrontendURL+"/login?ssoError="+url.QueryEscape(code))
}

func selectSSOMembership(memberships []domain.Membership, organizationID string) (domain.Membership, error) {
	if organizationID != "" {
		for _, membership := range memberships {
			if membership.OrganizationID == organizationID {
				return membership, nil
			}
		}
		return domain.Membership{}, errors.New("workspace_denied")
	}
	if len(memberships) == 1 {
		return memberships[0], nil
	}
	if len(memberships) == 0 {
		return domain.Membership{}, errors.New("account_not_found")
	}
	return domain.Membership{}, errors.New("workspace_required")
}
