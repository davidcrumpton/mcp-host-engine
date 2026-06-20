package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Identity struct {
	Progname  string `json:"prog"`
	Version   string `json:"v"`
	Username  string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func Create(progname, version, username string, ttl time.Duration, secret string) (string, error) {
	now := time.Now()
	payload, _ := json.Marshal(Identity{
		Progname:  progname,
		Version:   version,
		Username:  username,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func Validate(progname, version, token, secret string) (*Identity, error) {
	// Not validating version at this time, but in the future we will when the version
	// matters. It assists user in understanding what application the token is for
	// in a world filled with so many JSON tokens!

	if progname != "mcphe" {
		return nil, fmt.Errorf("invalid program name %s", progname)
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed token %s", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid payload base64 %s", payload)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid signature base64 %s", sig)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return nil, fmt.Errorf("invalid signature %s", sig)
	}

	var id Identity
	if err := json.Unmarshal(payload, &id); err != nil {
		return nil, fmt.Errorf("malformed payload %s", payload)
	}
	if time.Now().Unix() > id.ExpiresAt {
		return nil, fmt.Errorf("token expired for %s", id.Username)
	}
	return &id, nil
}
