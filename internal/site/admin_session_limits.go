package site

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errAdminSessionLimit = errors.New("too many login sessions; retry later")

type adminSessionWindow struct {
	since time.Time
	count int
}
type adminSessionLimiter struct {
	mu            sync.Mutex
	windows       map[string]adminSessionWindow
	loginWindows  map[string]adminSessionWindow
	loginInFlight int
	lastCleanup   time.Time
}

const (
	adminLoginAttemptsPerMinute = 10
	adminLoginMaxConcurrent     = 4
)

// Both login aliases share admission before any password verification. A valid
// anonymous session is not a rate-limit exemption. Bound concurrent expensive
// work across all peers as well as each peer's attempts and the peer map itself.
func (a *adminServer) beginLogin(c *gin.Context) bool {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = c.Request.RemoteAddr
	}
	now := time.Now()
	a.limits.mu.Lock()
	defer a.limits.mu.Unlock()
	if a.limits.loginInFlight >= adminLoginMaxConcurrent {
		return false
	}
	if a.limits.loginWindows == nil {
		a.limits.loginWindows = map[string]adminSessionWindow{}
	}
	for key, w := range a.limits.loginWindows {
		if now.Sub(w.since) >= time.Minute {
			delete(a.limits.loginWindows, key)
		}
	}
	w, ok := a.limits.loginWindows[host]
	if !ok {
		if len(a.limits.loginWindows) >= 4096 {
			return false
		}
		w = adminSessionWindow{since: now}
	}
	if w.count >= adminLoginAttemptsPerMinute {
		return false
	}
	w.count++
	a.limits.loginWindows[host] = w
	a.limits.loginInFlight++
	return true
}

func (a *adminServer) endLogin() {
	a.limits.mu.Lock()
	a.limits.loginInFlight--
	a.limits.mu.Unlock()
}

// Rate limiting is based on the socket peer, never spoofable forwarding headers.
// The map is bounded even under a flood of distinct source addresses.
func (a *adminServer) allowAnonymous(c *gin.Context) bool {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = c.Request.RemoteAddr
	}
	now := time.Now()
	a.limits.mu.Lock()
	defer a.limits.mu.Unlock()
	if a.limits.windows == nil {
		a.limits.windows = map[string]adminSessionWindow{}
	}
	for key, w := range a.limits.windows {
		if now.Sub(w.since) > time.Minute {
			delete(a.limits.windows, key)
		}
	}
	w, ok := a.limits.windows[host]
	if !ok {
		if len(a.limits.windows) >= 4096 {
			return false
		}
		w = adminSessionWindow{since: now}
	}
	if w.count >= 10 {
		return false
	}
	w.count++
	a.limits.windows[host] = w
	return true
}

// CleanupAdminSessions deletes at most limit expired rows per call; the worker
// calls this periodically and admin traffic also reclaims expired sessions.
func CleanupAdminSessions(ctx context.Context, db *gorm.DB, limit int) error {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var hashes []string
	if err := db.WithContext(ctx).Model(&AdminSession{}).Where("expires_at <= ?", time.Now().UTC()).Order("expires_at asc").Limit(limit).Pluck("token_hash", &hashes).Error; err != nil {
		return err
	}
	if len(hashes) == 0 {
		return nil
	}
	return db.WithContext(ctx).Where("token_hash IN ? AND expires_at <= ?", hashes, time.Now().UTC()).Delete(&AdminSession{}).Error
}

func (a *adminServer) cleanupSessions(ctx context.Context) error {
	a.limits.mu.Lock()
	due := time.Since(a.limits.lastCleanup) >= time.Minute
	if due {
		a.limits.lastCleanup = time.Now()
	}
	a.limits.mu.Unlock()
	if !due {
		return nil
	}
	return CleanupAdminSessions(ctx, a.runtime.db, 200)
}
