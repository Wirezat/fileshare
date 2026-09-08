package shared

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Argon2id parameters. Above the OWASP floor of m=19 MiB, t=2, p=1, and the
// same shape production-optimizer uses so the two projects stay comparable.
const (
	argonTime    uint32 = 2
	argonMemory  uint32 = 64 * 1024 // KiB
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

var errInvalidHash = errors.New("crypto: unrecognised password hash")

// HashPassword hashes a plaintext password with Argon2id.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("crypto: generating salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// CheckPassword reports whether password matches hash. It accepts both the
// current Argon2id format and the bcrypt hashes written by earlier versions,
// so an existing data.json keeps working untouched.
func CheckPassword(password, hash string) bool {
	if strings.HasPrefix(hash, "$argon2id$") {
		return checkArgon2(password, hash) == nil
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NeedsRehash reports whether a hash was written by an outdated scheme and
// should be replaced. Callers re-hash on a successful login, which is the only
// moment the plaintext is available.
func NeedsRehash(hash string) bool {
	return hash != "" && !strings.HasPrefix(hash, "$argon2id$")
}

func checkArgon2(password, encoded string) error {
	params, salt, want, err := decodeArgon2(encoded)
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return errors.New("crypto: password does not match")
	}
	return nil
}

type argon2Params struct {
	memory, time uint32
	threads      uint8
}

func decodeArgon2(encoded string) (p argon2Params, salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, errInvalidHash
	}
	var version int
	if _, err = fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return p, nil, nil, errInvalidHash
	}
	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return p, nil, nil, errInvalidHash
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return p, nil, nil, errInvalidHash
	}
	if hash, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return p, nil, nil, errInvalidHash
	}
	return p, salt, hash, nil
}
