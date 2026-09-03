package ojs

import "testing"

func TestAllErrorCodesCount(t *testing.T) {
	if got := len(AllErrorCodes); got != 54 {
		t.Errorf("AllErrorCodes has %d entries, want 54", got)
	}
}

func TestAllCodesAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(AllErrorCodes))
	for _, e := range AllErrorCodes {
		if seen[e.Code] {
			t.Errorf("duplicate Code: %s", e.Code)
		}
		seen[e.Code] = true
	}
}

func TestAllCanonicalCodesAreUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, e := range AllErrorCodes {
		if e.CanonicalCode == "" {
			continue
		}
		if seen[e.CanonicalCode] {
			t.Errorf("duplicate CanonicalCode: %s", e.CanonicalCode)
		}
		seen[e.CanonicalCode] = true
	}
}

func TestLookupByCode(t *testing.T) {
	tests := []struct {
		code     string
		wantNil  bool
		wantName string
	}{
		{"OJS-1000", false, "InvalidPayload"},
		{"OJS-3000", false, "JobNotFound"},
		{"OJS-7004", false, "MiddlewareTimeout"},
		{"OJS-9999", true, ""},
		{"", true, ""},
	}
	for _, tt := range tests {
		entry := LookupByCode(tt.code)
		if tt.wantNil {
			if entry != nil {
				t.Errorf("LookupByCode(%q) = %+v, want nil", tt.code, entry)
			}
		} else {
			if entry == nil {
				t.Fatalf("LookupByCode(%q) = nil, want entry", tt.code)
			}
			if entry.Name != tt.wantName {
				t.Errorf("LookupByCode(%q).Name = %q, want %q", tt.code, entry.Name, tt.wantName)
			}
		}
	}
}

func TestLookupByCanonicalCode(t *testing.T) {
	tests := []struct {
		canonical string
		wantNil   bool
		wantCode  string
	}{
		{"INVALID_PAYLOAD", false, "OJS-1000"},
		{"NOT_FOUND", false, "OJS-3000"},
		{"RATE_LIMITED", false, "OJS-6000"},
		{"DOES_NOT_EXIST", true, ""},
		{"", true, ""},
	}
	for _, tt := range tests {
		entry := LookupByCanonicalCode(tt.canonical)
		if tt.wantNil {
			if entry != nil {
				t.Errorf("LookupByCanonicalCode(%q) = %+v, want nil", tt.canonical, entry)
			}
		} else {
			if entry == nil {
				t.Fatalf("LookupByCanonicalCode(%q) = nil, want entry", tt.canonical)
			}
			if entry.Code != tt.wantCode {
				t.Errorf("LookupByCanonicalCode(%q).Code = %q, want %q", tt.canonical, entry.Code, tt.wantCode)
			}
		}
	}
}
