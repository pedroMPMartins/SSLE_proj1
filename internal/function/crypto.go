package function

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// SignPayload generates an HMAC-SHA256 signature for a given payload and timestamp.
// Signature = HMAC-SHA256(secretKey, timestampUnixStr + "." + payload)
func SignPayload(secretKey []byte, timestamp time.Time, body []byte) (timestampStr string, signatureHex string) {
	tsUnix := timestamp.Unix()
	timestampStr = strconv.FormatInt(tsUnix, 10)

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(timestampStr))
	mac.Write([]byte("."))
	mac.Write(body)

	signatureHex = hex.EncodeToString(mac.Sum(nil))
	return timestampStr, signatureHex
}

// VerifyPayload verifies the HMAC-SHA256 signature and checks for timestamp drift to prevent replay attacks.
func VerifyPayload(secretKey []byte, timestampStr, signatureHex string, body []byte, maxDrift time.Duration) (bool, string) {
	if len(secretKey) == 0 {
		return false, "empty HMAC secret key"
	}
	if timestampStr == "" {
		return false, "missing X-Signature-Timestamp header"
	}
	if signatureHex == "" {
		return false, "missing X-Signature header"
	}

	// 1. Verify Timestamp & Replay Window
	tsUnix, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return false, "malformed timestamp header"
	}

	reqTime := time.Unix(tsUnix, 0)
	now := time.Now()
	drift := now.Sub(reqTime)
	if drift < 0 {
		drift = -drift
	}
	if maxDrift > 0 && drift > maxDrift {
		return false, fmt.Sprintf("timestamp drift exceeded (%.1fs > %.1fs): potential replay attack", drift.Seconds(), maxDrift.Seconds())
	}

	// 2. Compute Expected Signature
	expectedMac := hmac.New(sha256.New, secretKey)
	expectedMac.Write([]byte(timestampStr))
	expectedMac.Write([]byte("."))
	expectedMac.Write(body)
	expectedBytes := expectedMac.Sum(nil)

	providedBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false, "malformed hex signature"
	}

	// 3. Constant-Time Comparison to prevent timing side-channel attacks
	if !hmac.Equal(providedBytes, expectedBytes) {
		return false, "signature mismatch: payload modified in transit or invalid secret key"
	}

	return true, ""
}
