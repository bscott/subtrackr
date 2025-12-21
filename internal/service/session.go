package service

import (
	"net/http"

	"github.com/gorilla/sessions"
)

const (
	SessionName     = "subtrackr_session"
	SessionUserKey  = "user_authenticated"
	SessionMaxAge   = 24 * 60 * 60 // 24 hours in seconds
	RememberMeMaxAge = 30 * 24 * 60 * 60 // 30 days in seconds
)

type SessionService struct {
	store *sessions.CookieStore
}

// NewSessionService creates a new session service
func NewSessionService(secretKey string) *SessionService {
	store := sessions.NewCookieStore([]byte(secretKey))

	// Configure session options
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   SessionMaxAge,
		HttpOnly: true,
		Secure:   false, // Set to true if using HTTPS
		SameSite: http.SameSiteStrictMode,
	}

	return &SessionService{store: store}
}

// CreateSession creates a new authenticated session
func (s *SessionService) CreateSession(w http.ResponseWriter, r *http.Request, rememberMe bool) error {
	session, err := s.store.Get(r, SessionName)
	if err != nil {
		return err
	}

	session.Values[SessionUserKey] = true

	// Extend session if "remember me" is checked
	if rememberMe {
		session.Options.MaxAge = RememberMeMaxAge
	} else {
		session.Options.MaxAge = SessionMaxAge
	}

	return session.Save(r, w)
}

// IsAuthenticated checks if the user is authenticated
func (s *SessionService) IsAuthenticated(r *http.Request) bool {
	session, err := s.store.Get(r, SessionName)
	if err != nil {
		return false
	}

	auth, ok := session.Values[SessionUserKey].(bool)
	return ok && auth
}

// DestroySession destroys the user session
func (s *SessionService) DestroySession(w http.ResponseWriter, r *http.Request) error {
	session, err := s.store.Get(r, SessionName)
	if err != nil {
		return err
	}

	// Mark session as expired
	session.Options.MaxAge = -1
	delete(session.Values, SessionUserKey)

	return session.Save(r, w)
}

// RefreshSession extends the session expiration
func (s *SessionService) RefreshSession(w http.ResponseWriter, r *http.Request) error {
	session, err := s.store.Get(r, SessionName)
	if err != nil {
		return err
	}

	// Only refresh if authenticated
	if auth, ok := session.Values[SessionUserKey].(bool); ok && auth {
		// Extend the max age
		currentMaxAge := session.Options.MaxAge
		if currentMaxAge > 0 {
			session.Options.MaxAge = currentMaxAge
		}
		return session.Save(r, w)
	}

	return nil
}

// UpdateSessionExpiry updates the session secret (useful when secret changes)
func (s *SessionService) UpdateSessionExpiry(maxAge int) {
	s.store.Options.MaxAge = maxAge
}

// GetSession retrieves the current session
func (s *SessionService) GetSession(r *http.Request) (*sessions.Session, error) {
	return s.store.Get(r, SessionName)
}
