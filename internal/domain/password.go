package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// ErrBadCredentials is returned when a password sign-in fails: an unknown
// email, an Account with no password, a wrong password, or a locked Account.
// It never says which, so the sign-in page can't be used to learn who has an
// Account.
var ErrBadCredentials = errors.New("bad email or password")

// ErrLinkInvalid is returned for a set-password link that is unknown, expired,
// or already used.
var ErrLinkInvalid = errors.New("set-password link is invalid")

// passwordLinkLife is how long a set-password link works after it's issued.
const passwordLinkLife = 72 * time.Hour

// IssuePasswordLink issues a set-password link for emailAddr, creating the
// Account through EnsureAccount when there is none, and replacing any link the
// Account had. It returns the link's token, which is stored only as its hash,
// and when the link expires.
func (s *Service) IssuePasswordLink(ctx context.Context, emailAddr string) (string, time.Time, error) {
	acc, err := s.EnsureAccount(ctx, emailAddr)
	if err != nil {
		return "", time.Time{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("make link token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := s.clock.Now().UTC().Add(passwordLinkLife)
	if err := s.queries.UpsertPasswordLink(ctx, db.UpsertPasswordLinkParams{
		AccountID: acc.ID,
		TokenHash: passwordLinkHash(token),
		ExpiresAt: expires.Format(timeFormat),
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("store password link: %w", err)
	}
	return token, expires, nil
}

// PasswordLinkAccount is the Account a set-password link sets the password of.
// It returns ErrLinkInvalid for a link that is unknown, expired, or used.
func (s *Service) PasswordLinkAccount(ctx context.Context, token string) (Account, error) {
	link, err := s.queries.GetPasswordLink(ctx, passwordLinkHash(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrLinkInvalid
	}
	if err != nil {
		return Account{}, fmt.Errorf("get password link: %w", err)
	}
	return s.Account(ctx, link.AccountID)
}

// SetPassword sets the password of the Account the set-password link is for,
// using the link up. It returns ErrLinkInvalid for a link that is unknown,
// expired, or used.
func (s *Service) SetPassword(ctx context.Context, token, password string) (Account, error) {
	acc, err := s.PasswordLinkAccount(ctx, token)
	if err != nil {
		return Account{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Account{}, err
	}
	err = s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.queries.SetAccountPassword(ctx, db.SetAccountPasswordParams{AccountID: acc.ID, PasswordHash: &hash}); err != nil {
			return fmt.Errorf("store password: %w", err)
		}
		if err := tx.queries.DeletePasswordLink(ctx, acc.ID); err != nil {
			return fmt.Errorf("use up password link: %w", err)
		}
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return acc, nil
}

// PasswordSignIn resolves a password sign-in. It never creates an Account.
func (s *Service) PasswordSignIn(ctx context.Context, emailAddr, password string) (Account, error) {
	row, err := s.queries.GetAccountByEmail(ctx, emailAddr)
	if err != nil {
		return Account{}, fmt.Errorf("look up account: %w", err)
	}
	stored, err := s.queries.GetAccountPassword(ctx, row.ID)
	if err != nil {
		return Account{}, fmt.Errorf("get password: %w", err)
	}
	if !passwordMatches(*stored.PasswordHash, password) {
		return Account{}, ErrBadCredentials
	}
	return accountFromRow(row), nil
}

// passwordLinkHash is how a set-password link's token is stored: its SHA-256,
// in hex.
func passwordLinkHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// The argon2id parameters passwords are hashed with.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 1
	argonThreads = 4
	argonSaltLen = 16
	argonKeyLen  = 32
)

// hashPassword is password hashed with argon2id under a fresh random salt, as
// a PHC string: $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("make salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// passwordMatches reports whether password hashes to the PHC string hash,
// comparing in constant time. A hash it can't read matches nothing.
func passwordMatches(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
