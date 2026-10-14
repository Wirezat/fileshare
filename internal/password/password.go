package password

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

// Argon2id parameters, above the OWASP floor of m=19 MiB, t=2, p=1.
const (
	argonTime    uint32 = 2
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

var errInvalidHash = errors.New("password: unrecognised hash")

// Hash returns an Argon2id hash of pw in PHC string format.
func Hash(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: generating salt: %w", err)
	}
	hash := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// Check reports whether pw matches hash; hash may be Argon2id or legacy bcrypt.
func Check(pw, hash string) bool {
	if strings.HasPrefix(hash, "$argon2id$") {
		return checkArgon2(pw, hash) == nil
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// NeedsRehash reports whether a non-empty hash was written by an outdated scheme.
func NeedsRehash(hash string) bool {
	return hash != "" && !strings.HasPrefix(hash, "$argon2id$")
}

func checkArgon2(pw, encoded string) error {
	params, salt, want, err := decodeArgon2(encoded)
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(pw), salt, params.time, params.memory, params.threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return errors.New("password: does not match")
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
