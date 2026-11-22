package auth

import (
	"strings"
	"testing"
)

// Every error from Validate is logged by transport.ValidateToken, so none of
// them may quote the token: the messages used to include the raw payload and
// signature segments "for debugging", which made a failed login a token
// disclosure in the log.
func TestValidateErrorsNeverContainTheToken(t *testing.T) {
	secret := "test-secret"
	good, err := Create("mcphe", "test", "alice", "laptop", 3600, secret)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(good, ".", 2)

	cases := map[string]string{
		"not a token at all":       "garbage",
		"payload is not base64":    "!!!.AAAA",
		"signature is not base64":  parts[0] + ".!!!",
		"signature does not match": parts[0] + "." + parts[1][:len(parts[1])-2] + "AA",
		"payload is not json":      "eyJub3QiOiJqc29uIn0.AAAA",
	}

	for name, token := range cases {
		_, err := Validate("mcphe", "test", token, secret, nil)
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}
		msg := err.Error()
		for _, segment := range strings.SplitN(token, ".", 2) {
			if len(segment) > 3 && strings.Contains(msg, segment) {
				t.Errorf("%s: error message quotes the token segment %q: %s", name, segment, msg)
			}
		}
	}
}
