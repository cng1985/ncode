package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cng1985/ncode/internal/config"
	"github.com/cng1985/ncode/internal/model"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SessionCookie = "ncode_session"
	sessionTTL    = 12 * time.Hour
	verifyTTL     = 30 * time.Minute
)

type Service struct {
	db         *gorm.DB
	secureOnly bool
	limits     *rateLimiter
}

var Module = fx.Module("identity", fx.Provide(New))

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]attemptWindow
}

type attemptWindow struct {
	started time.Time
	count   int
}

func New(db *gorm.DB, cfg config.Config) *Service {
	return &Service{db: db, secureOnly: cfg.AppEnv == "production", limits: &rateLimiter{entries: make(map[string]attemptWindow)}}
}

// Middleware authenticates a browser session and resolves an active workspace
// membership for every protected API call.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rawToken, err := c.Cookie(SessionCookie)
		if err != nil || rawToken == "" {
			unauthorized(c)
			c.Abort()
			return
		}
		var session model.Session
		now := time.Now().UTC()
		if err := s.db.Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash(rawToken), now).First(&session).Error; err != nil {
			unauthorized(c)
			c.Abort()
			return
		}
		var user model.User
		if err := s.db.First(&user, session.UserID).Error; err != nil || user.EmailVerifiedAt == nil {
			unauthorized(c)
			c.Abort()
			return
		}

		memberships := s.db.Model(&model.Membership{}).Where("user_id = ? AND state = ?", user.ID, "active")
		if workspaceHeader := strings.TrimSpace(c.GetHeader("X-Workspace-ID")); workspaceHeader != "" {
			workspaceID, parseErr := strconv.ParseInt(workspaceHeader, 10, 64)
			if parseErr != nil || !model.SafeID(workspaceID) {
				forbidden(c)
				c.Abort()
				return
			}
			memberships = memberships.Where("tenant_id = ?", workspaceID)
		}
		var membership model.Membership
		if err := memberships.Order("created_at ASC").First(&membership).Error; err != nil {
			forbidden(c)
			c.Abort()
			return
		}
		c.Set("identity.userID", user.ID)
		c.Set("identity.userEmail", user.Email)
		c.Set("identity.userName", user.Name)
		c.Set("identity.workspaceID", membership.TenantID)
		c.Set("identity.role", membership.Role)
		c.Set("identity.sessionID", session.ID)
		if now.Sub(session.LastSeenAt) > time.Minute {
			_ = s.db.Model(&session).Update("last_seen_at", now).Error
		}
		c.Next()
	}
}

// RegisterPublicRoutes installs the registration, verification, and login API.
func (s *Service) RegisterPublicRoutes(r *gin.RouterGroup) {
	r.POST("/auth/register", s.register)
	r.POST("/auth/verify", s.verifyEmail)
	r.POST("/auth/login", s.login)
}

// RegisterRoutes installs authenticated account and workspace APIs.
func (s *Service) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/auth/me", s.me)
	r.POST("/auth/logout", s.logout)
	r.POST("/workspace/create", s.createWorkspace)
	r.POST("/workspace/invite/create", s.createInvitation)
	r.POST("/workspace/invite/accept", s.acceptInvitation)
	r.POST("/workspace/invite/revoke", s.revokeInvitation)
}

func (s *Service) register(c *gin.Context) {
	if s.secureOnly {
		failure(c, http.StatusServiceUnavailable, 50001, "email verification delivery is not configured")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decodeJSON(c, &req) {
		return
	}
	if !s.allow("register:"+clientIP(c), 5, time.Hour) {
		failure(c, http.StatusTooManyRequests, 43003, "too many registration attempts; try again later")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Name = strings.TrimSpace(req.Name)
	parsed, err := mail.ParseAddress(req.Email)
	if err != nil || parsed.Address != req.Email || len(req.Email) > 254 || req.Name == "" || len([]rune(req.Name)) > 80 || len(req.Password) < 12 || len(req.Password) > 72 {
		failure(c, http.StatusBadRequest, 41001, "provide a valid email, name, and password of 12 to 72 bytes")
		return
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		internalFailure(c)
		return
	}
	verificationToken, err := randomToken()
	if err != nil {
		internalFailure(c)
		return
	}
	var user model.User
	err = s.db.Transaction(func(tx *gorm.DB) error {
		user = model.User{Email: req.Email, Name: req.Name, PasswordHash: string(passwordHash)}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		tenant := model.Tenant{Name: req.Name + "的个人空间", OwnerUserID: user.ID, Kind: "personal"}
		if err := tx.Create(&tenant).Error; err != nil {
			return err
		}
		membership := model.Membership{TenantID: tenant.ID, UserID: user.ID, Role: "owner", State: "active"}
		if err := tx.Create(&membership).Error; err != nil {
			return err
		}
		verification := model.EmailVerification{UserID: user.ID, TokenHash: tokenHash(verificationToken), ExpiresAt: time.Now().UTC().Add(verifyTTL)}
		return tx.Create(&verification).Error
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			failure(c, http.StatusConflict, 42002, "an account with this email already exists")
			return
		}
		internalFailure(c)
		return
	}
	data := gin.H{"email": user.Email, "verificationExpiresInSeconds": int(verifyTTL.Seconds())}
	// The development response makes local onboarding testable without an SMTP
	// server. Production never returns verification credentials in the API.
	if !s.secureOnly {
		data["developmentVerificationToken"] = verificationToken
	}
	success(c, data)
}

func (s *Service) verifyEmail(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if !decodeJSON(c, &req) {
		return
	}
	if len(req.Token) < 32 || len(req.Token) > 128 {
		failure(c, http.StatusBadRequest, 41001, "verification token is invalid or expired")
		return
	}
	var user model.User
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var verification model.EmailVerification
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", tokenHash(req.Token), time.Now().UTC()).First(&verification).Error; err != nil {
			return err
		}
		var account model.User
		if err := tx.First(&account, verification.UserID).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&verification).Update("used_at", now).Error; err != nil {
			return err
		}
		if err := tx.Model(&account).Update("email_verified_at", now).Error; err != nil {
			return err
		}
		user = account
		user.EmailVerifiedAt = &now
		return nil
	})
	if err != nil {
		failure(c, http.StatusBadRequest, 43001, "verification token is invalid or expired")
		return
	}
	if !s.issueSession(c, user) {
		return
	}
	success(c, gin.H{"id": user.ID, "email": user.Email, "name": user.Name})
}

func (s *Service) login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(c, &req) {
		return
	}
	key := "login:" + clientIP(c) + ":" + strings.ToLower(strings.TrimSpace(req.Email))
	ipKey := "login-ip:" + clientIP(c)
	if !s.allow(ipKey, 40, 15*time.Minute) || !s.allow(key, 10, 15*time.Minute) {
		failure(c, http.StatusTooManyRequests, 43003, "too many sign-in attempts; try again later")
		return
	}
	var user model.User
	err := s.db.Where("email = ?", strings.ToLower(strings.TrimSpace(req.Email))).First(&user).Error
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		failure(c, http.StatusUnauthorized, 43001, "email or password is incorrect")
		return
	}
	if user.EmailVerifiedAt == nil {
		failure(c, http.StatusForbidden, 43002, "verify your email before signing in")
		return
	}
	s.limits.reset(key)
	s.limits.reset(ipKey)
	if !s.issueSession(c, user) {
		return
	}
	success(c, gin.H{"id": user.ID, "email": user.Email, "name": user.Name})
}

func (s *Service) issueSession(c *gin.Context, user model.User) bool {
	raw, err := randomToken()
	if err != nil {
		internalFailure(c)
		return false
	}
	now := time.Now().UTC()
	session := model.Session{UserID: user.ID, TokenHash: tokenHash(raw), ExpiresAt: now.Add(sessionTTL), LastSeenAt: now}
	if err := s.db.Create(&session).Error; err != nil {
		internalFailure(c)
		return false
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: SessionCookie, Value: raw, Path: "/", Expires: session.ExpiresAt, MaxAge: int(sessionTTL.Seconds()), HttpOnly: true, Secure: s.secureOnly, SameSite: http.SameSiteStrictMode})
	return true
}

func (s *Service) me(c *gin.Context) {
	userID := UserID(c)
	var user model.User
	if err := s.db.First(&user, userID).Error; err != nil {
		internalFailure(c)
		return
	}
	type workspaceView struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
		Role string `json:"role"`
	}
	var workspaces []workspaceView
	err := s.db.Table("memberships").Select("tenants.id, tenants.name, tenants.kind, memberships.role").
		Joins("JOIN tenants ON tenants.id = memberships.tenant_id").Where("memberships.user_id = ? AND memberships.state = ?", userID, "active").Order("memberships.created_at ASC").Scan(&workspaces).Error
	if err != nil {
		internalFailure(c)
		return
	}
	success(c, gin.H{"id": user.ID, "email": user.Email, "name": user.Name, "workspaces": workspaces, "currentWorkspaceId": WorkspaceID(c)})
}

func (s *Service) createWorkspace(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(c, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 120 {
		failure(c, http.StatusBadRequest, 41001, "workspace name is required and must be at most 120 characters")
		return
	}
	userID := UserID(c)
	var tenant model.Tenant
	err := s.db.Transaction(func(tx *gorm.DB) error {
		tenant = model.Tenant{Name: req.Name, OwnerUserID: userID, Kind: "team"}
		if err := tx.Create(&tenant).Error; err != nil {
			return err
		}
		return tx.Create(&model.Membership{TenantID: tenant.ID, UserID: userID, Role: "owner", State: "active"}).Error
	})
	if err != nil {
		internalFailure(c)
		return
	}
	success(c, gin.H{"id": tenant.ID, "name": tenant.Name, "kind": tenant.Kind, "role": "owner"})
}

func (s *Service) createInvitation(c *gin.Context) {
	if s.secureOnly {
		failure(c, http.StatusServiceUnavailable, 50001, "email delivery is not configured")
		return
	}
	if !canManageInvitations(CurrentRole(c)) {
		forbidden(c)
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decodeJSON(c, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Role == "" {
		req.Role = "member"
	}
	parsed, err := mail.ParseAddress(req.Email)
	if err != nil || parsed.Address != req.Email || len(req.Email) > 254 || (req.Role != "member" && req.Role != "admin") || (CurrentRole(c) == "admin" && req.Role != "member") {
		failure(c, http.StatusBadRequest, 41001, "valid email and a permitted workspace role are required")
		return
	}
	key := "invite:" + strconv.FormatInt(UserID(c), 10)
	if !s.allow(key, 20, time.Hour) {
		failure(c, http.StatusTooManyRequests, 43003, "too many invitations; try again later")
		return
	}
	var existing int64
	err = s.db.Table("memberships").Joins("JOIN users ON users.id = memberships.user_id").Where("memberships.tenant_id = ? AND users.email = ? AND memberships.state = ?", WorkspaceID(c), req.Email, "active").Count(&existing).Error
	if err != nil {
		internalFailure(c)
		return
	}
	if existing > 0 {
		failure(c, http.StatusConflict, 42002, "this person already belongs to the workspace")
		return
	}
	var pending int64
	if err := s.db.Model(&model.WorkspaceInvitation{}).Where("tenant_id = ? AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?", WorkspaceID(c), time.Now().UTC()).Count(&pending).Error; err != nil {
		internalFailure(c)
		return
	}
	if pending >= 100 {
		failure(c, http.StatusConflict, 42001, "workspace has reached its active invitation limit")
		return
	}
	rawToken, err := randomToken()
	if err != nil {
		internalFailure(c)
		return
	}
	now := time.Now().UTC()
	invitation := model.WorkspaceInvitation{TenantID: WorkspaceID(c), InvitedEmail: req.Email, Role: req.Role, TokenHash: tokenHash(rawToken), InvitedBy: UserID(c), ExpiresAt: now.Add(7 * 24 * time.Hour)}
	if err := s.db.Create(&invitation).Error; err != nil {
		internalFailure(c)
		return
	}
	success(c, gin.H{"id": invitation.ID, "email": invitation.InvitedEmail, "role": invitation.Role, "expiresAt": invitation.ExpiresAt, "developmentInvitationToken": rawToken})
}

func (s *Service) acceptInvitation(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if !decodeJSON(c, &req) {
		return
	}
	if len(req.Token) < 32 || len(req.Token) > 128 {
		failure(c, http.StatusBadRequest, 41001, "invitation token is invalid or expired")
		return
	}
	var accepted model.WorkspaceInvitation
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var invitation model.WorkspaceInvitation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ? AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?", tokenHash(req.Token), time.Now().UTC()).First(&invitation).Error; err != nil {
			return err
		}
		var user model.User
		if err := tx.First(&user, UserID(c)).Error; err != nil {
			return err
		}
		if user.EmailVerifiedAt == nil || user.Email != invitation.InvitedEmail {
			return gorm.ErrRecordNotFound
		}
		membership := model.Membership{TenantID: invitation.TenantID, UserID: user.ID, Role: invitation.Role, State: "active"}
		if err := tx.Create(&membership).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&invitation).Update("accepted_at", now).Error; err != nil {
			return err
		}
		accepted = invitation
		return nil
	})
	if err != nil {
		failure(c, http.StatusConflict, 43005, "invitation is invalid, expired, already used, or addressed to another account")
		return
	}
	var tenant model.Tenant
	if err := s.db.First(&tenant, accepted.TenantID).Error; err != nil {
		internalFailure(c)
		return
	}
	success(c, gin.H{"id": tenant.ID, "name": tenant.Name, "kind": tenant.Kind, "role": accepted.Role})
}

func (s *Service) revokeInvitation(c *gin.Context) {
	if !canManageInvitations(CurrentRole(c)) {
		forbidden(c)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeJSON(c, &req) {
		return
	}
	if !model.SafeID(req.ID) {
		failure(c, http.StatusBadRequest, 41001, "invitation id is invalid")
		return
	}
	now := time.Now().UTC()
	result := s.db.Model(&model.WorkspaceInvitation{}).Where("id = ? AND tenant_id = ? AND accepted_at IS NULL AND revoked_at IS NULL", req.ID, WorkspaceID(c)).Update("revoked_at", now)
	if result.Error != nil {
		internalFailure(c)
		return
	}
	if result.RowsAffected == 0 {
		failure(c, http.StatusNotFound, 42002, "invitation not found")
		return
	}
	success(c, gin.H{"id": req.ID, "revoked": true})
}

func (s *Service) logout(c *gin.Context) {
	if sessionID, ok := c.Get("identity.sessionID"); ok {
		now := time.Now().UTC()
		if err := s.db.Model(&model.Session{}).Where("id = ? AND revoked_at IS NULL", sessionID).Update("revoked_at", now).Error; err != nil {
			internalFailure(c)
			return
		}
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: s.secureOnly, SameSite: http.SameSiteStrictMode})
	success(c, gin.H{"revoked": true})
}

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	entry := l.entries[key]
	if entry.started.IsZero() && len(l.entries) >= 50000 {
		for existing, value := range l.entries {
			if now.Sub(value.started) > time.Hour {
				delete(l.entries, existing)
			}
		}
		if len(l.entries) >= 50000 {
			return false
		}
	}
	if entry.started.IsZero() || now.Sub(entry.started) >= window {
		entry = attemptWindow{started: now}
	}
	entry.count++
	l.entries[key] = entry
	return entry.count <= limit
}

func (s *Service) allow(key string, limit int, window time.Duration) bool {
	return s.limits.allow(key, limit, window)
}

func (l *rateLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func UserID(c *gin.Context) int64 {
	value, _ := c.Get("identity.userID")
	id, _ := value.(int64)
	return id
}
func WorkspaceID(c *gin.Context) int64 {
	value, _ := c.Get("identity.workspaceID")
	id, _ := value.(int64)
	return id
}
func CurrentRole(c *gin.Context) string {
	value, _ := c.Get("identity.role")
	role, _ := value.(string)
	return role
}
func UserName(c *gin.Context) string {
	value, _ := c.Get("identity.userName")
	name, _ := value.(string)
	return name
}

func decodeJSON(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := c.ShouldBindJSON(target); err != nil {
		failure(c, http.StatusBadRequest, 41001, "invalid request")
		return false
	}
	return true
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func clientIP(c *gin.Context) string { return c.ClientIP() }

func canManageInvitations(role string) bool { return role == "owner" || role == "admin" }

func unauthorized(c *gin.Context) { failure(c, http.StatusUnauthorized, 43001, "sign in is required") }
func forbidden(c *gin.Context) {
	failure(c, http.StatusForbidden, 43004, "workspace access is not allowed")
}
func internalFailure(c *gin.Context) {
	failure(c, http.StatusInternalServerError, 50001, "request could not be completed")
}
func success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "success", "data": data})
}
func failure(c *gin.Context, status, code int, message string) {
	c.JSON(status, gin.H{"code": code, "msg": message, "data": nil})
}
