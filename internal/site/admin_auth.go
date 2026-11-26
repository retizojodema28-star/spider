package site

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AdminUser struct {
	ID             uint   `gorm:"primaryKey"`
	Username       string `gorm:"size:100;uniqueIndex;not null"`
	PasswordHash   string `gorm:"size:100;not null" json:"-"`
	FailedAttempts int    `gorm:"not null"`
	LockedUntil    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AdminSession struct {
	TokenHash string    `gorm:"primaryKey;size:64" json:"-"`
	UserID    uint      `gorm:"index;not null"`
	CSRF      string    `gorm:"size:64;not null" json:"-"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
}

type AdminOptions struct {
	// Origin pins the externally visible origin behind a reverse proxy. Forwarded
	// headers are deliberately not trusted for authentication or cookie policy.
	Origin        string
	CookieName    string
	SessionTTL    time.Duration
	SecureCookies bool
}

func ValidateAdminEnvironment() error {
	name := strings.TrimSpace(os.Getenv("ADMIN_USERNAME"))
	password := os.Getenv("ADMIN_PASSWORD")
	hash := os.Getenv("ADMIN_PASSWORD_HASH")
	if name == "" || len(name) > 100 || strings.ContainsAny(name, " \t\r\n\x00") {
		return fmt.Errorf("ADMIN_USERNAME is required and must be a simple name")
	}
	if password != "" && hash != "" {
		return fmt.Errorf("set ADMIN_PASSWORD or ADMIN_PASSWORD_HASH, not both")
	}
	if hash != "" {
		cost, err := bcrypt.Cost([]byte(hash))
		if err != nil || cost < 10 || cost > 16 {
			return fmt.Errorf("ADMIN_PASSWORD_HASH must be bcrypt with cost 10..16")
		}
	} else {
		if len(password) < 12 || len(password) > 72 {
			return fmt.Errorf("ADMIN_PASSWORD must contain 12..72 bytes")
		}
		lower := strings.ToLower(password)
		if strings.Contains(lower, "changeme") || strings.Contains(lower, "password") || strings.Contains(lower, "example") || strings.Contains(lower, "123456") || strings.Contains(lower, "admin") || strings.EqualFold(password, name) {
			return fmt.Errorf("ADMIN_PASSWORD must not be a default or predictable password")
		}
		unique := map[rune]bool{}
		for _, c := range password {
			unique[c] = true
		}
		if len(unique) < 8 {
			return fmt.Errorf("ADMIN_PASSWORD has insufficient diversity")
		}
	}
	origin := os.Getenv("ADMIN_ORIGIN")
	dev := os.Getenv("APP_ENV") == "development" || os.Getenv("APP_ENV") == "test"
	if !dev && origin == "" {
		return fmt.Errorf("ADMIN_ORIGIN is required in production")
	}
	if origin != "" {
		u, err := parseAdminOrigin(origin)
		if err != nil {
			return err
		}
		if !dev && u.Scheme != "https" {
			return fmt.Errorf("production ADMIN_ORIGIN must use HTTPS")
		}
	}
	return nil
}

func parseAdminOrigin(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("ADMIN_ORIGIN must be an HTTP(S) origin without path")
	}
	if _, err := normalizeAdminDomain(u.Host); err != nil {
		return nil, fmt.Errorf("invalid ADMIN_ORIGIN host")
	}
	// Match browser origin serialization without collapsing distinct schemes or
	// nondefault ports. Keep a trailing DNS dot: it is part of an origin's host.
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Is6() != strings.HasPrefix(u.Host, "[") {
			return nil, fmt.Errorf("ADMIN_ORIGIN IPv6 addresses must use brackets; IPv4 addresses must not")
		}
		host = ip.String()
		if ip.Is4In6() {
			// URL serialization uses hexadecimal pieces even for IPv4-mapped
			// IPv6; netip.String instead emits a dotted-decimal suffix.
			b := ip.As16()
			host = fmt.Sprintf("::ffff:%x:%x", uint16(b[12])<<8|uint16(b[13]), uint16(b[14])<<8|uint16(b[15]))
		}
	} else {
		if strings.HasPrefix(u.Host, "[") {
			return nil, fmt.Errorf("invalid ADMIN_ORIGIN IPv6 address")
		}
		// Browsers parse hosts ending in a number as IPv4, not DNS. Do not
		// silently accept short, integer, octal or hex spellings which would
		// serialize differently; only canonical four-octet IPv4 is supported.
		last := strings.TrimSuffix(host, ".")
		last = last[strings.LastIndexByte(last, '.')+1:]
		decimal := last != "" && strings.Trim(last, "0123456789") == ""
		hex := strings.HasPrefix(last, "0x") && strings.Trim(last[2:], "0123456789abcdef") == ""
		if decimal || hex {
			return nil, fmt.Errorf("ADMIN_ORIGIN IPv4 must use four decimal octets without leading zeros or a trailing dot (for example 127.0.0.1)")
		}
	}
	if port != "" {
		n, _ := strconv.Atoi(port) // range and syntax validated above
		port = strconv.Itoa(n)
		if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
			port = ""
		}
	}
	u.Host = host
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	return u, nil
}

func AdminOptionsFromEnv() (AdminOptions, error) {
	if err := ValidateAdminEnvironment(); err != nil {
		return AdminOptions{}, err
	}
	origin := os.Getenv("ADMIN_ORIGIN")
	if origin != "" {
		u, err := parseAdminOrigin(origin)
		if err != nil {
			return AdminOptions{}, err
		}
		origin = u.String()
	}
	return AdminOptions{Origin: origin, SecureCookies: strings.HasPrefix(origin, "https://")}, nil
}

// BootstrapAdminFromEnv never resets an existing account on restart. Credentials
// are hashed before insertion and never appear in logs or response payloads.
func BootstrapAdminFromEnv(ctx context.Context, db *gorm.DB) error {
	if err := ValidateAdminEnvironment(); err != nil {
		return err
	}
	name := strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_USERNAME")))
	var existing AdminUser
	if err := db.WithContext(ctx).Where("username = ?", name).First(&existing).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	hash := os.Getenv("ADMIN_PASSWORD_HASH")
	if hash == "" {
		b, err := bcrypt.GenerateFromPassword([]byte(os.Getenv("ADMIN_PASSWORD")), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		hash = string(b)
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&AdminUser{Username: name, PasswordHash: hash}).Error
}

func adminToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func adminTokenHash(token string) string {
	b := sha256.Sum256([]byte(token))
	return hex.EncodeToString(b[:])
}

type adminServer struct {
	limits    adminSessionLimiter
	runtime   *Runtime
	opts      AdminOptions
	dummyHash []byte
}

func adminError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"ok": false, "message": message})
}
func adminOK(c *gin.Context, data any) { c.JSON(http.StatusOK, gin.H{"ok": true, "data": data}) }

func adminDecode(c *gin.Context, value any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		adminError(c, 400, "invalid JSON body or unknown field")
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		adminError(c, 400, "expected one JSON object")
		return false
	}
	return true
}

func (a *adminServer) secure(c *gin.Context) bool {
	return a.opts.SecureCookies || c.Request.TLS != nil
}
func (a *adminServer) cookie(c *gin.Context, token string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: a.opts.CookieName, Value: token, Path: "/admin", HttpOnly: true, Secure: a.secure(c), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (a *adminServer) session(c *gin.Context) (AdminSession, error) {
	token, err := c.Cookie(a.opts.CookieName)
	if err != nil || len(token) != 64 {
		return AdminSession{}, gorm.ErrRecordNotFound
	}
	var s AdminSession
	err = a.runtime.db.WithContext(c.Request.Context()).Where("token_hash = ? AND expires_at > ?", adminTokenHash(token), time.Now().UTC()).First(&s).Error
	return s, err
}

func (a *adminServer) newSession(c *gin.Context, userID uint, old *AdminSession) (AdminSession, error) {
	if userID == 0 && !a.allowAnonymous(c) {
		return AdminSession{}, errAdminSessionLimit
	}
	token, err := adminToken()
	if err != nil {
		return AdminSession{}, err
	}
	csrf, err := adminToken()
	if err != nil {
		return AdminSession{}, err
	}
	ttl := a.opts.SessionTTL
	if userID == 0 {
		ttl = 15 * time.Minute
	}
	s := AdminSession{TokenHash: adminTokenHash(token), CSRF: csrf, UserID: userID, ExpiresAt: time.Now().UTC().Add(ttl)}
	err = a.runtime.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if userID == 0 {
			var rev RuntimeRevision
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rev, 1).Error; err != nil {
				return err
			}
			if err := CleanupAdminSessions(c.Request.Context(), tx, 200); err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&AdminSession{}).Where("user_id = ?", 0).Count(&count).Error; err != nil {
				return err
			}
			if count >= 1000 {
				return errAdminSessionLimit
			}
		}
		if old != nil {
			if err := tx.Delete(&AdminSession{}, "token_hash = ?", old.TokenHash).Error; err != nil {
				return err
			}
		}
		return tx.Create(&s).Error
	})
	if err == nil {
		a.cookie(c, token, int(ttl.Seconds()))
	}
	return s, err
}

func (a *adminServer) sessionInfo(c *gin.Context) {
	if err := a.cleanupSessions(c.Request.Context()); err != nil {
		adminError(c, 503, "session storage unavailable")
		return
	}
	s, err := a.session(c)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s, err = a.newSession(c, 0, nil)
	}
	if err != nil {
		if errors.Is(err, errAdminSessionLimit) {
			c.Header("Retry-After", "60")
			adminError(c, 429, err.Error())
		} else {
			adminError(c, 503, "session storage unavailable")
		}
		return
	}
	adminOK(c, gin.H{"authenticated": s.UserID != 0, "csrf_token": s.CSRF, "expires_at": s.ExpiresAt})
}

func (a *adminServer) originOK(c *gin.Context) bool {
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	want := a.opts.Origin
	if want == "" {
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		want = scheme + "://" + c.Request.Host
	}
	got := c.GetHeader("Origin")
	if got == "" {
		u, err := url.Parse(c.GetHeader("Referer"))
		if err != nil || u.Host == "" || u.User != nil {
			return false
		}
		got = u.Scheme + "://" + u.Host
	}
	wantURL, wantErr := parseAdminOrigin(want)
	gotURL, gotErr := parseAdminOrigin(got)
	return wantErr == nil && gotErr == nil && gotURL.String() == wantURL.String()
}

func (a *adminServer) require(auth bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		s, err := a.session(c)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				adminError(c, 503, "session storage unavailable")
			} else {
				adminError(c, 401, "session required")
			}
			return
		}
		if auth && s.UserID == 0 {
			adminError(c, 401, "login required")
			return
		}
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			token := c.GetHeader("X-CSRF-Token")
			if !a.originOK(c) || subtle.ConstantTimeCompare([]byte(token), []byte(s.CSRF)) != 1 {
				adminError(c, 403, "invalid origin or CSRF token")
				return
			}
		}
		c.Set("admin_session", s)
		c.Next()
	}
}

func (a *adminServer) login(c *gin.Context) {
	if !a.beginLogin(c) {
		c.Header("Retry-After", "60")
		adminError(c, 429, "too many login attempts; retry later")
		return
	}
	defer a.endLogin()
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !adminDecode(c, &body) {
		return
	}
	if len(body.Password) > 72 {
		adminError(c, 401, "invalid credentials")
		return
	}
	var user AdminUser
	err := a.runtime.db.WithContext(c.Request.Context()).Where("username = ?", strings.ToLower(strings.TrimSpace(body.Username))).First(&user).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		adminError(c, 503, "authentication storage unavailable")
		return
	}
	now := time.Now().UTC()
	if user.LockedUntil != nil && user.LockedUntil.After(now) {
		adminError(c, 429, "account temporarily locked; retry after 15 minutes")
		return
	}
	hash := []byte(user.PasswordHash)
	if err != nil {
		hash = a.dummyHash
	}
	match := bcrypt.CompareHashAndPassword(hash, []byte(body.Password)) == nil
	if err != nil || !match {
		if user.ID != 0 {
			e := a.runtime.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
				var locked AdminUser
				if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, user.ID).Error; e != nil {
					return e
				}
				if locked.LockedUntil != nil && !locked.LockedUntil.After(now) {
					locked.FailedAttempts = 0
					locked.LockedUntil = nil
				}
				locked.FailedAttempts++
				if locked.FailedAttempts >= 5 {
					until := now.Add(15 * time.Minute)
					locked.LockedUntil = &until
				}
				return tx.Save(&locked).Error
			})
			if e != nil {
				adminError(c, 503, "authentication storage unavailable")
				return
			}
		}
		adminError(c, 401, "invalid credentials")
		return
	}
	if err := a.runtime.db.WithContext(c.Request.Context()).Model(&user).Updates(map[string]any{"failed_attempts": 0, "locked_until": nil}).Error; err != nil {
		adminError(c, 503, "authentication storage unavailable")
		return
	}
	old := c.MustGet("admin_session").(AdminSession)
	s, err := a.newSession(c, user.ID, &old)
	if err != nil {
		adminError(c, 503, "session storage unavailable")
		return
	}
	adminOK(c, gin.H{"authenticated": true, "csrf_token": s.CSRF, "expires_at": s.ExpiresAt})
}

func (a *adminServer) logout(c *gin.Context) {
	s := c.MustGet("admin_session").(AdminSession)
	if err := a.runtime.db.WithContext(c.Request.Context()).Delete(&AdminSession{}, "token_hash = ?", s.TokenHash).Error; err != nil {
		adminError(c, 503, "session storage unavailable")
		return
	}
	a.cookie(c, "", -1)
	adminOK(c, gin.H{"authenticated": false})
}
