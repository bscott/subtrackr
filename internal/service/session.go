package service

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/sessions"
)

const (
	SessionName      = "subtrackr_session"
	SessionUserKey   = "user_authenticated"
	SessionMaxAge    = 24 * 60 * 60      // 24 hours in seconds
	RememberMeMaxAge = 30 * 24 * 60 * 60 // 30 days in seconds
	OIDCSessionName  = "subtrackr_oidc"
	OIDCCallbackPath = "/auth/oidc/callback"
	OIDCFlowMaxAge   = 5 * 60
	oidcStateKey     = "oidc_state"
	oidcNonceKey     = "oidc_nonce"
	oidcVerifierKey  = "oidc_code_verifier"
	oidcRedirectKey  = "oidc_redirect"
	oidcIssuedAtKey  = "oidc_issued_at"
)

// OIDCFlow contains the browser-bound values required to complete one OIDC login.
type OIDCFlow struct {
	Nonce        string
	CodeVerifier string
	Redirect     string
}

type SessionService struct {
	store     *sessions.CookieStore
	oidcStore *sessions.CookieStore
	now       func() time.Time
}

// NewSessionService creates a new session service.
func NewSessionService(secretKey string) *SessionService {
	store := sessions.NewCookieStore([]byte(secretKey))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   SessionMaxAge,
		HttpOnly: true,
		Secure:   false, // Set to true if using HTTPS
		SameSite: http.SameSiteLaxMode,
	}

	// The OIDC transaction uses independent derived authentication and
	// encryption keys so its PKCE verifier and nonce are confidential even
	// though the application session remains backward compatible.
	oidcStore := sessions.NewCookieStore(
		oidcKey(secretKey, "authentication"),
		oidcKey(secretKey, "encryption"),
	)
	oidcStore.Options = oidcCookieOptions(false, OIDCFlowMaxAge)
	oidcStore.MaxAge(OIDCFlowMaxAge)

	return &SessionService{store: store, oidcStore: oidcStore, now: time.Now}
}

func oidcKey(secretKey, purpose string) []byte {
	sum := sha256.Sum256([]byte("subtrackr oidc " + purpose + "\x00" + secretKey))
	return sum[:]
}

func oidcCookieOptions(secure bool, maxAge int) *sessions.Options {
	return &sessions.Options{
		Path:     OIDCCallbackPath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// CreateSession creates a new authenticated session.
func (s *SessionService) CreateSession(w http.ResponseWriter, r *http.Request, rememberMe, secure bool) error {
	session, err := s.store.Get(r, SessionName)
	if err != nil {
		return err
	}

	session.Values[SessionUserKey] = true
	session.Options.Secure = secure

	if rememberMe {
		session.Options.MaxAge = RememberMeMaxAge
	} else {
		session.Options.MaxAge = SessionMaxAge
	}

	return session.Save(r, w)
}

// SaveOIDCFlow stores one OIDC authorization flow in a dedicated encrypted cookie.
func (s *SessionService) SaveOIDCFlow(w http.ResponseWriter, r *http.Request, state string, flow *OIDCFlow, secure bool) error {
	if state == "" || flow == nil || flow.Nonce == "" || flow.CodeVerifier == "" {
		return fmt.Errorf("invalid OIDC flow")
	}

	session, err := s.oidcStore.Get(r, OIDCSessionName)
	if err != nil {
		return err
	}
	session.Values[oidcStateKey] = state
	session.Values[oidcNonceKey] = flow.Nonce
	session.Values[oidcVerifierKey] = flow.CodeVerifier
	session.Values[oidcRedirectKey] = flow.Redirect
	session.Values[oidcIssuedAtKey] = s.now().Unix()
	session.Options = oidcCookieOptions(secure, OIDCFlowMaxAge)
	return session.Save(r, w)
}

// ConsumeOIDCFlow validates state and clears the transaction cookie on every outcome.
func (s *SessionService) ConsumeOIDCFlow(w http.ResponseWriter, r *http.Request, state string, secure bool) (*OIDCFlow, error) {
	session, err := s.oidcStore.Get(r, OIDCSessionName)
	s.clearOIDCFlowCookie(w, secure)
	if err != nil {
		return nil, err
	}

	expectedState, ok := session.Values[oidcStateKey].(string)
	if !ok || expectedState == "" || subtle.ConstantTimeCompare([]byte(expectedState), []byte(state)) != 1 {
		return nil, fmt.Errorf("invalid OIDC state")
	}

	flow := &OIDCFlow{
		Nonce:        stringSessionValue(session.Values[oidcNonceKey]),
		CodeVerifier: stringSessionValue(session.Values[oidcVerifierKey]),
		Redirect:     stringSessionValue(session.Values[oidcRedirectKey]),
	}
	if flow.Nonce == "" || flow.CodeVerifier == "" {
		return nil, fmt.Errorf("incomplete OIDC flow")
	}

	issuedAt, ok := session.Values[oidcIssuedAtKey].(int64)
	now := s.now().Unix()
	if !ok || issuedAt <= 0 || now-issuedAt > OIDCFlowMaxAge || issuedAt > now+30 {
		return nil, fmt.Errorf("expired OIDC flow")
	}
	return flow, nil
}

func (s *SessionService) clearOIDCFlowCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, sessions.NewCookie(OIDCSessionName, "", oidcCookieOptions(secure, -1)))
}

func stringSessionValue(value interface{}) string {
	result, _ := value.(string)
	return result
}

// IsAuthenticated checks if the user is authenticated.
func (s *SessionService) IsAuthenticated(r *http.Request) bool {
	session, err := s.store.Get(r, SessionName)
	if err != nil {
		return false
	}

	auth, ok := session.Values[SessionUserKey].(bool)
	return ok && auth
}

// DestroySession destroys the user session.
func (s *SessionService) DestroySession(w http.ResponseWriter, r *http.Request) error {
	session, err := s.store.Get(r, SessionName)
	if err != nil {
		return err
	}

	session.Options.MaxAge = -1
	delete(session.Values, SessionUserKey)

	return session.Save(r, w)
}

// RefreshSession extends the session expiration.
func (s *SessionService) RefreshSession(w http.ResponseWriter, r *http.Request) error {
	session, err := s.store.Get(r, SessionName)
	if err != nil {
		return err
	}

	if auth, ok := session.Values[SessionUserKey].(bool); ok && auth {
		currentMaxAge := session.Options.MaxAge
		if currentMaxAge > 0 {
			session.Options.MaxAge = currentMaxAge
		}
		return session.Save(r, w)
	}

	return nil
}

// UpdateSessionExpiry updates the session secret (useful when secret changes).
func (s *SessionService) UpdateSessionExpiry(maxAge int) {
	s.store.Options.MaxAge = maxAge
}

// GetSession retrieves the current session.
func (s *SessionService) GetSession(r *http.Request) (*sessions.Session, error) {
	return s.store.Get(r, SessionName)
}
