package service

import (
	"errors"
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPAuth returns an smtp.Auth that negotiates the authentication mechanism
// based on what the server advertises in its EHLO response. It prefers PLAIN,
// but falls back to LOGIN when the server does not offer PLAIN.
//
// This matters for Office 365 / Outlook (smtp.office365.com), which advertises
// only "LOGIN" and "XOAUTH2" and rejects an AUTH PLAIN attempt with
// "504 5.7.4 Unrecognized authentication type". Go's net/smtp only ships PLAIN
// and CRAM-MD5, so LOGIN is implemented here.
func SMTPAuth(host, username, password string) smtp.Auth {
	return &autoAuth{host: host, username: username, password: password}
}

// autoAuth picks PLAIN or LOGIN at negotiation time, once the server's
// advertised mechanism list is available via *smtp.ServerInfo.
type autoAuth struct {
	host     string
	username string
	password string
	chosen   smtp.Auth
}

func (a *autoAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	hasPlain, hasLogin := false, false
	for _, m := range server.Auth {
		switch strings.ToUpper(strings.TrimSpace(m)) {
		case "PLAIN":
			hasPlain = true
		case "LOGIN":
			hasLogin = true
		}
	}

	switch {
	case hasPlain:
		a.chosen = smtp.PlainAuth("", a.username, a.password, a.host)
	case hasLogin:
		a.chosen = &loginAuth{username: a.username, password: a.password}
	default:
		// Server didn't advertise a mechanism we recognize; default to PLAIN so
		// the server returns a meaningful error rather than us guessing.
		a.chosen = smtp.PlainAuth("", a.username, a.password, a.host)
	}

	return a.chosen.Start(server)
}

func (a *autoAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	return a.chosen.Next(fromServer, more)
}

// loginAuth implements the SMTP AUTH LOGIN mechanism. The server prompts for the
// username and then the password (each as a base64-encoded challenge, which
// net/smtp decodes before calling Next).
type loginAuth struct {
	username string
	password string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	// Refuse to send credentials in the clear; every SubTrackr SMTP path runs
	// over implicit TLS or STARTTLS before authenticating.
	if !server.TLS {
		return "", nil, errors.New("smtp: refusing to send LOGIN credentials over an unencrypted connection")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	prompt := strings.ToLower(strings.TrimSpace(string(fromServer)))
	switch {
	case strings.HasPrefix(prompt, "user"):
		return []byte(a.username), nil
	case strings.HasPrefix(prompt, "pass"):
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("smtp: unexpected LOGIN challenge from server: %q", string(fromServer))
	}
}
