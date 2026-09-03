package serverless

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	// PushTimestampHeader carries the Unix-seconds timestamp signed by an OJS
	// push producer.
	PushTimestampHeader = "X-OJS-Timestamp"

	// PushSignatureHeader carries one or more comma-separated sha256 signatures.
	PushSignatureHeader = "X-OJS-Signature"

	maxPushTimestampHeaderBytes = 32
	maxPushSignatureHeaderBytes = 8 << 10
	maxPushSignatures           = 32
)

var (
	errPushAuthNotConfigured  = errors.New("push authentication is not configured")
	errPushAuthInvalidConfig  = errors.New("push authentication configuration is invalid")
	errPushAuthInvalid        = errors.New("invalid push authentication")
	errPushAuthHeaderTooLarge = errors.New("push authentication header is too large")
)

func (h *LambdaHandler) authenticatePush(timestampHeader string, signatureHeaders []string, body []byte, now time.Time) error {
	if h.insecureAllowUnsignedPushForLocalDevelopment {
		return nil
	}
	if len(h.pushSigningSecrets) == 0 {
		return errPushAuthNotConfigured
	}
	if h.pushFreshnessWindow <= 0 {
		return errPushAuthInvalidConfig
	}

	timestamp, err := parsePushTimestamp(timestampHeader)
	if err != nil {
		return err
	}
	if timestamp.Before(now.Add(-h.pushFreshnessWindow)) ||
		timestamp.After(now.Add(h.pushFreshnessWindow)) {
		return errPushAuthInvalid
	}

	signatures, err := parsePushSignatures(signatureHeaders)
	if err != nil {
		return err
	}

	signedPrefix := timestampHeader + "."
	matched := false
	for _, secret := range h.pushSigningSecrets {
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte(signedPrefix))
		_, _ = mac.Write(body)
		expected := mac.Sum(nil)
		for _, signature := range signatures {
			if hmac.Equal(expected, signature) {
				matched = true
			}
		}
	}
	if !matched {
		return errPushAuthInvalid
	}
	return nil
}

func parsePushTimestamp(value string) (time.Time, error) {
	if value == "" || len(value) > maxPushTimestampHeaderBytes {
		if len(value) > maxPushTimestampHeaderBytes {
			return time.Time{}, errPushAuthHeaderTooLarge
		}
		return time.Time{}, errPushAuthInvalid
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return time.Time{}, errPushAuthInvalid
		}
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, errPushAuthInvalid
	}
	return time.Unix(seconds, 0), nil
}

func parsePushSignatures(values []string) ([][]byte, error) {
	if len(values) == 0 {
		return nil, errPushAuthInvalid
	}

	totalBytes := 0
	signatures := make([][]byte, 0, len(values))
	for _, value := range values {
		totalBytes += len(value)
		if totalBytes > maxPushSignatureHeaderBytes {
			return nil, errPushAuthHeaderTooLarge
		}
		for _, part := range strings.Split(value, ",") {
			if len(signatures) >= maxPushSignatures {
				return nil, errPushAuthHeaderTooLarge
			}
			part = strings.TrimSpace(part)
			if len(part) != len("sha256=")+sha256.Size*2 ||
				!strings.HasPrefix(part, "sha256=") {
				return nil, errPushAuthInvalid
			}
			signature, err := hex.DecodeString(strings.TrimPrefix(part, "sha256="))
			if err != nil || len(signature) != sha256.Size {
				return nil, errPushAuthInvalid
			}
			signatures = append(signatures, signature)
		}
	}
	if len(signatures) == 0 {
		return nil, errPushAuthInvalid
	}
	return signatures, nil
}
