package service

import (
	"net/smtp"
	"testing"
)

func TestAutoAuthChoosesPlainWhenAdvertised(t *testing.T) {
	auth := SMTPAuth("mail.example.com", "user", "pass")
	server := &smtp.ServerInfo{Name: "mail.example.com", TLS: true, Auth: []string{"LOGIN", "PLAIN"}}

	mech, _, err := auth.Start(server)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if mech != "PLAIN" {
		t.Errorf("mechanism = %q, want PLAIN (should prefer PLAIN when both are offered)", mech)
	}
}

func TestAutoAuthFallsBackToLoginForOffice365(t *testing.T) {
	auth := SMTPAuth("smtp.office365.com", "user@example.com", "pass")
	// Office 365 advertises LOGIN and XOAUTH2, but not PLAIN.
	server := &smtp.ServerInfo{Name: "smtp.office365.com", TLS: true, Auth: []string{"LOGIN", "XOAUTH2"}}

	mech, _, err := auth.Start(server)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if mech != "LOGIN" {
		t.Errorf("mechanism = %q, want LOGIN", mech)
	}
}

func TestAutoAuthDefaultsToPlainForUnknownMechanisms(t *testing.T) {
	auth := SMTPAuth("mail.example.com", "user", "pass")
	server := &smtp.ServerInfo{Name: "mail.example.com", TLS: true, Auth: []string{"XOAUTH2"}}

	mech, _, err := auth.Start(server)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if mech != "PLAIN" {
		t.Errorf("mechanism = %q, want PLAIN as the fallback default", mech)
	}
}

func TestLoginAuthRefusesUnencryptedConnection(t *testing.T) {
	auth := &loginAuth{username: "user", password: "pass"}
	server := &smtp.ServerInfo{Name: "mail.example.com", TLS: false}

	if _, _, err := auth.Start(server); err == nil {
		t.Error("Start should refuse to proceed when server.TLS is false")
	}
}

func TestLoginAuthNextRespondsToChallenges(t *testing.T) {
	auth := &loginAuth{username: "user@example.com", password: "s3cret"}

	got, err := auth.Next([]byte("Username:"), true)
	if err != nil {
		t.Fatalf("Next(Username:) returned error: %v", err)
	}
	if string(got) != "user@example.com" {
		t.Errorf("Next(Username:) = %q, want %q", got, "user@example.com")
	}

	got, err = auth.Next([]byte("Password:"), true)
	if err != nil {
		t.Fatalf("Next(Password:) returned error: %v", err)
	}
	if string(got) != "s3cret" {
		t.Errorf("Next(Password:) = %q, want %q", got, "s3cret")
	}

	if _, err := auth.Next([]byte("Something else:"), true); err == nil {
		t.Error("Next should error on an unrecognized challenge")
	}

	got, err = auth.Next(nil, false)
	if err != nil {
		t.Fatalf("Next(more=false) returned error: %v", err)
	}
	if got != nil {
		t.Errorf("Next(more=false) = %q, want nil", got)
	}
}
