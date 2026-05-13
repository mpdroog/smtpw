package main

import (
	"strings"
	"testing"

	"github.com/mpdroog/smtpw/config"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple filename", "document.pdf", "document.pdf"},
		{"with spaces", "my document.pdf", "my_document.pdf"},
		{"path traversal attempt", "../../../etc/passwd", "passwd"},
		{"path traversal with backslash", "..\\..\\etc\\passwd", ".._.._etc_passwd"}, // backslash not a separator on Unix
		{"absolute path", "/etc/passwd", "passwd"},
		{"windows path", "C:\\Users\\test\\file.txt", "C__Users_test_file.txt"}, // backslash not a separator on Unix
		{"special characters", "file<>:\"|?*.txt", "file_______.txt"},
		{"unicode characters", "файл.txt", "____.txt"},
		{"triple dots", "...", "..."},
		{"just dots", "..", "attachment"},
		{"single dot", ".", "attachment"},
		{"empty string", "", "attachment"},
		{"hidden file", ".hidden", ".hidden"},
		{"long filename truncated", strings.Repeat("a", 300), strings.Repeat("a", 255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   config.Email
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid email",
			email: config.Email{
				From:    "support",
				To:      []string{"test@example.com"},
				Subject: "Test",
				Text:    "Hello",
			},
			wantErr: false,
		},
		{
			name: "text body too large",
			email: config.Email{
				From:    "support",
				To:      []string{"test@example.com"},
				Subject: "Test",
				Text:    strings.Repeat("a", config.MaxBodySize+1),
			},
			wantErr: true,
			errMsg:  "text body exceeds max size",
		},
		{
			name: "html body too large",
			email: config.Email{
				From:    "support",
				To:      []string{"test@example.com"},
				Subject: "Test",
				Html:    strings.Repeat("a", config.MaxBodySize+1),
			},
			wantErr: true,
			errMsg:  "html body exceeds max size",
		},
		{
			name: "too many recipients",
			email: config.Email{
				From:    "support",
				To:      makeRecipients(config.MaxRecipients + 1),
				Subject: "Test",
				Text:    "Hello",
			},
			wantErr: true,
			errMsg:  "too many recipients",
		},
		{
			name: "too many recipients with BCC",
			email: config.Email{
				From:    "support",
				To:      makeRecipients(50),
				BCC:     makeRecipients(51),
				Subject: "Test",
				Text:    "Hello",
			},
			wantErr: true,
			errMsg:  "too many recipients",
		},
		{
			name: "too many attachments",
			email: config.Email{
				From:        "support",
				To:          []string{"test@example.com"},
				Subject:     "Test",
				Text:        "Hello",
				Attachments: makeAttachments(config.MaxAttachments + 1),
			},
			wantErr: true,
			errMsg:  "too many attachments",
		},
		{
			name: "attachment too large",
			email: config.Email{
				From:    "support",
				To:      []string{"test@example.com"},
				Subject: "Test",
				Text:    "Hello",
				Attachments: map[string]string{
					"large.bin": strings.Repeat("a", config.MaxAttachmentSize+1),
				},
			},
			wantErr: true,
			errMsg:  "exceeds max size",
		},
		{
			name: "embed too large",
			email: config.Email{
				From:    "support",
				To:      []string{"test@example.com"},
				Subject: "Test",
				Html:    "<img src=\"cid:large.png\">",
				HtmlEmbed: map[string]string{
					"large.png": strings.Repeat("a", config.MaxAttachmentSize+1),
				},
			},
			wantErr: true,
			errMsg:  "exceeds max size",
		},
		{
			name: "max recipients allowed",
			email: config.Email{
				From:    "support",
				To:      makeRecipients(config.MaxRecipients),
				Subject: "Test",
				Text:    "Hello",
			},
			wantErr: false,
		},
		{
			name: "max attachments allowed",
			email: config.Email{
				From:        "support",
				To:          []string{"test@example.com"},
				Subject:     "Test",
				Text:        "Hello",
				Attachments: makeAttachments(config.MaxAttachments),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEmail(&tt.email)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateEmail() expected error containing %q, got nil", tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateEmail() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateEmail() unexpected error: %v", err)
				}
			}
		})
	}
}

func makeRecipients(n int) []string {
	recipients := make([]string, n)
	for i := 0; i < n; i++ {
		recipients[i] = "test@example.com"
	}
	return recipients
}

func makeAttachments(n int) map[string]string {
	attachments := make(map[string]string)
	for i := 0; i < n; i++ {
		attachments[string(rune('a'+i%26))+string(rune('0'+i/26))+".txt"] = "data"
	}
	return attachments
}
