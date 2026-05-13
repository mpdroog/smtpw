package main

import (
	"strings"
	"testing"
)

func TestRandText(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"zero length", 0},
		{"short", 8},
		{"medium", 32},
		{"long", 128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RandText(tt.length)
			if len(result) != tt.length {
				t.Errorf("RandText(%d) returned length %d, want %d", tt.length, len(result), tt.length)
			}
		})
	}
}

func TestRandTextCharset(t *testing.T) {
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := RandText(1000)

	for _, c := range result {
		if !strings.ContainsRune(validChars, c) {
			t.Errorf("RandText() contains invalid character: %c", c)
		}
	}
}

func TestRandTextUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	iterations := 100

	for i := 0; i < iterations; i++ {
		result := RandText(32)
		if seen[result] {
			t.Errorf("RandText() produced duplicate value: %s", result)
		}
		seen[result] = true
	}
}

func TestRandTextNotEmpty(t *testing.T) {
	result := RandText(10)
	if result == "" {
		t.Error("RandText(10) returned empty string")
	}
	if result == strings.Repeat("a", 10) {
		t.Error("RandText() returned non-random string")
	}
}
