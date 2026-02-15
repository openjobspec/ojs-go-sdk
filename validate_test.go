package ojs

import (
	"strings"
	"testing"
)

func TestValidateEnqueueParams(t *testing.T) {
	tests := []struct {
		name    string
		jobType string
		args    []any
		wantErr bool
	}{
		{"valid type and args", "email.send", []any{"user@test.com"}, false},
		{"valid empty args", "process", []any{}, false},
		{"empty type", "", []any{"x"}, true},
		{"nil args", "test", nil, true},
		{"invalid type pattern", "EmailSend", []any{"x"}, true},
		{"type too long", strings.Repeat("a", 256), []any{"x"}, true},
		{"type at max length", strings.Repeat("a", 255), []any{"x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEnqueueParams(tt.jobType, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEnqueueParams() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateQueue(t *testing.T) {
	tests := []struct {
		name    string
		queue   string
		wantErr bool
	}{
		{"valid queue", "default", false},
		{"empty queue (default)", "", false},
		{"valid with hyphens", "my-queue", false},
		{"valid with dots", "queue.v2", false},
		{"uppercase invalid", "MyQueue", true},
		{"too long", strings.Repeat("a", 129), true},
		{"at max length", strings.Repeat("a", 128), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQueue(tt.queue)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateQueue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
