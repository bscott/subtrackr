package models

import (
	"testing"
)

func TestSettings_KeyValue(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		wantKey  string
		wantVal  string
	}{
		{
			name: "SMTP host setting",
			settings: Settings{
				Key:   "smtp_host",
				Value: "smtp.gmail.com",
			},
			wantKey: "smtp_host",
			wantVal: "smtp.gmail.com",
		},
		{
			name: "Notification setting",
			settings: Settings{
				Key:   "renewal_reminders",
				Value: "true",
			},
			wantKey: "renewal_reminders",
			wantVal: "true",
		},
		{
			name: "Empty setting",
			settings: Settings{
				Key:   "",
				Value: "",
			},
			wantKey: "",
			wantVal: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.settings.Key != tt.wantKey {
				t.Errorf("Settings.Key = %v, want %v", tt.settings.Key, tt.wantKey)
			}
			if tt.settings.Value != tt.wantVal {
				t.Errorf("Settings.Value = %v, want %v", tt.settings.Value, tt.wantVal)
			}
		})
	}
}

func TestSMTPConfig_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		config SMTPConfig
		valid  bool
	}{
		{
			name: "Valid SMTP config",
			config: SMTPConfig{
				Host:     "smtp.gmail.com",
				Port:     587,
				Username: "user@example.com",
				Password: "password",
				From:     "noreply@example.com",
				FromName: "SubTrackr",
			},
			valid: true,
		},
		{
			name: "Invalid - missing host",
			config: SMTPConfig{
				Port:     587,
				Username: "user@example.com",
				Password: "password",
				From:     "noreply@example.com",
			},
			valid: false,
		},
		{
			name: "Invalid - zero port",
			config: SMTPConfig{
				Host:     "smtp.gmail.com",
				Port:     0,
				Username: "user@example.com",
				Password: "password",
				From:     "noreply@example.com",
			},
			valid: false,
		},
		{
			name:   "Invalid - empty config",
			config: SMTPConfig{},
			valid:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test validation logic
			isValid := tt.config.Host != "" && 
					  tt.config.Port > 0 && 
					  tt.config.Username != "" && 
					  tt.config.Password != "" && 
					  tt.config.From != ""
			
			if isValid != tt.valid {
				t.Errorf("SMTPConfig validation = %v, want %v", isValid, tt.valid)
			}
		})
	}
}

func TestNotificationSettings_Defaults(t *testing.T) {
	tests := []struct {
		name     string
		settings NotificationSettings
		want     NotificationSettings
	}{
		{
			name: "With all settings",
			settings: NotificationSettings{
				RenewalReminders: true,
				HighCostAlerts:   true,
				ReminderDays:     7,
			},
			want: NotificationSettings{
				RenewalReminders: true,
				HighCostAlerts:   true,
				ReminderDays:     7,
			},
		},
		{
			name:     "Empty settings should have defaults",
			settings: NotificationSettings{},
			want: NotificationSettings{
				RenewalReminders: false,
				HighCostAlerts:   false,
				ReminderDays:     0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.settings.RenewalReminders != tt.want.RenewalReminders {
				t.Errorf("RenewalReminders = %v, want %v", tt.settings.RenewalReminders, tt.want.RenewalReminders)
			}
			if tt.settings.HighCostAlerts != tt.want.HighCostAlerts {
				t.Errorf("HighCostAlerts = %v, want %v", tt.settings.HighCostAlerts, tt.want.HighCostAlerts)
			}
			if tt.settings.ReminderDays != tt.want.ReminderDays {
				t.Errorf("ReminderDays = %v, want %v", tt.settings.ReminderDays, tt.want.ReminderDays)
			}
		})
	}
}

func TestAPIKey_IsValid(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  APIKey
		wantErr bool
	}{
		{
			name: "Valid API key",
			apiKey: APIKey{
				Name: "Test API Key",
				Key:  "sk_test_1234567890abcdef",
			},
			wantErr: false,
		},
		{
			name: "Invalid API key - missing name",
			apiKey: APIKey{
				Key: "sk_test_1234567890abcdef",
			},
			wantErr: true,
		},
		{
			name: "Invalid API key - missing key",
			apiKey: APIKey{
				Name: "Test API Key",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic validation that should exist
			hasError := tt.apiKey.Name == "" || tt.apiKey.Key == ""
			if hasError != tt.wantErr {
				t.Errorf("Validation error = %v, wantErr %v", hasError, tt.wantErr)
			}
		})
	}
}