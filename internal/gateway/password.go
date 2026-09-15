package gateway

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// passwordIterations is OWASP's figure for PBKDF2-HMAC-SHA256. A variable so
// the tests do not spend a second on every hash; the iteration count is kept
// in each hash, so raising it later does not lock anyone out.
var passwordIterations = 600_000

const (
	passwordScheme    = "pbkdf2-sha256" // #nosec G101 -- the name of the hash scheme, not a credential
	passwordSaltBytes = 16
	passwordKeyBytes  = 32
	minPasswordLength = 8
	// maxPasswordLength bounds the work one sign-in attempt can ask for.
	maxPasswordLength = 256
)

var passwordEncoding = base64.RawStdEncoding

// validatePassword says what is wrong with a new password, if anything.
func validatePassword(password string) error {
	if utf8.RuneCountInString(password) < minPasswordLength {
		return fmt.Errorf("a password needs at least %d characters", minPasswordLength)
	}
	if len(password) > maxPasswordLength {
		return fmt.Errorf("a password can be at most %d bytes long", maxPasswordLength)
	}
	return nil
}

// hashPassword derives a salted hash to store in place of the password.
func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s$%d$%s$%s", passwordScheme, passwordIterations,
		passwordEncoding.EncodeToString(salt), passwordEncoding.EncodeToString(key)), nil
}

// checkPassword reports whether password is the one encoded was made from.
func checkPassword(encoded, password string) bool {
	if len(password) > maxPasswordLength {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordScheme {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 || iterations > 10_000_000 {
		return false
	}
	salt, err := passwordEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := passwordEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

var (
	decoyOnce sync.Once
	decoy     string
)

// wasteTime checks a password against a hash nobody has, so that a sign-in as
// a user who does not exist takes as long to fail as one with a wrong
// password -- otherwise the time taken would say which usernames are real.
func wasteTime(password string) {
	decoyOnce.Do(func() { decoy, _ = hashPassword("k8sdockside decoy password") })
	checkPassword(decoy, password)
}
