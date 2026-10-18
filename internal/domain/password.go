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
	"unicode/utf8"

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

// ErrPasswordRules is returned for a password too short or too long; its
// message after "password rules: " says which, without the password.
var ErrPasswordRules = errors.New("password rules")

// The bounds of a password: at least passwordMinRunes characters, at most
// passwordMaxBytes bytes.
const (
	passwordMinRunes = 12
	passwordMaxBytes = 1024
)

// checkPasswordRules returns ErrPasswordRules, saying why, unless password is
// within the bounds.
func checkPasswordRules(password string) error {
	if utf8.RuneCountInString(password) < passwordMinRunes {
		return fmt.Errorf("%w: a password needs at least %d characters", ErrPasswordRules, passwordMinRunes)
	}
	if len(password) > passwordMaxBytes {
		return fmt.Errorf("%w: a password can be at most %d bytes", ErrPasswordRules, passwordMaxBytes)
	}
	return nil
}

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
	if acc.Departed {
		return "", time.Time{}, fmt.Errorf("%w: %s has left the company", ErrDeparted, acc.Email)
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
	expires, err := time.Parse(timeFormat, link.ExpiresAt)
	if err != nil {
		return Account{}, fmt.Errorf("read password link expiry: %w", err)
	}
	if !s.clock.Now().Before(expires) {
		return Account{}, ErrLinkInvalid
	}
	return s.Account(ctx, link.AccountID)
}

// SetPassword sets the password of the Account the set-password link is for:
// it stores the hash, lifts any lock, uses the link up, and ends every session
// the person had (EndSessions), so a browser signed in before is signed out.
// It returns ErrLinkInvalid for a link that is unknown, expired, or used,
// ErrPasswordRules for a password too short or long, leaving the link to try
// again, and ErrDeparted for a Departed person.
func (s *Service) SetPassword(ctx context.Context, token, password string) (Account, error) {
	acc, err := s.PasswordLinkAccount(ctx, token)
	if err != nil {
		return Account{}, err
	}
	if acc.Departed {
		return Account{}, fmt.Errorf("%w: %s has left the company", ErrDeparted, acc.Email)
	}
	if err := checkPasswordRules(password); err != nil {
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
		return tx.EndSessions(ctx, acc.ID)
	})
	if err != nil {
		return Account{}, err
	}
	return acc, nil
}

// PasswordSignIn resolves a password sign-in for emailAddr. An unknown email,
// an Account with no password, a wrong password, and an Account locked by
// wrong ones all return ErrBadCredentials, and each takes as long, since each
// hashes the password. Each wrong password counts toward the lockout; the
// right one clears the count. A Departed person with the right password is
// refused with ErrDeparted. It never creates an Account.
func (s *Service) PasswordSignIn(ctx context.Context, emailAddr, password string) (Account, error) {
	row, err := s.queries.GetAccountByEmail(ctx, emailAddr)
	if errors.Is(err, sql.ErrNoRows) {
		passwordMatches(dummyPasswordHash, password)
		return Account{}, ErrBadCredentials
	}
	if err != nil {
		return Account{}, fmt.Errorf("look up account: %w", err)
	}
	stored, err := s.queries.GetAccountPassword(ctx, row.ID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && stored.PasswordHash == nil {
		passwordMatches(dummyPasswordHash, password)
		return Account{}, ErrBadCredentials
	}
	if err != nil {
		return Account{}, fmt.Errorf("get password: %w", err)
	}
	matches := passwordMatches(*stored.PasswordHash, password)
	now := s.clock.Now()
	locked, err := lockedAt(stored, now)
	if err != nil {
		return Account{}, err
	}
	if locked {
		return Account{}, ErrBadCredentials
	}
	if !matches {
		lockedUntil := now.UTC().Add(signInLockout).Format(timeFormat)
		if err := s.queries.RecordFailedSignin(ctx, db.RecordFailedSigninParams{
			LockAt: signInLockAt, LockedUntil: &lockedUntil, AccountID: row.ID,
		}); err != nil {
			return Account{}, fmt.Errorf("record failed sign-in: %w", err)
		}
		return Account{}, ErrBadCredentials
	}
	if stored.FailedSignins != 0 || stored.LockedUntil != nil {
		if err := s.queries.ClearFailedSignins(ctx, row.ID); err != nil {
			return Account{}, fmt.Errorf("clear failed sign-ins: %w", err)
		}
	}
	acc := accountFromRow(row)
	if acc.Departed {
		return Account{}, fmt.Errorf("%w: %s has left the company", ErrDeparted, acc.Email)
	}
	return acc, nil
}

// The lockout: signInLockAt wrong passwords in a row lock an Account for
// signInLockout, even against the right one.
const (
	signInLockAt  = 5
	signInLockout = 15 * time.Minute
)

// lockedAt reports whether the Account stored is for is locked at now.
func lockedAt(stored db.AccountPassword, now time.Time) (bool, error) {
	if stored.LockedUntil == nil {
		return false, nil
	}
	until, err := time.Parse(timeFormat, *stored.LockedUntil)
	if err != nil {
		return false, fmt.Errorf("read lock: %w", err)
	}
	return now.Before(until), nil
}

// dummyPasswordHash is what a sign-in with no password to check hashes the
// given one against, so it takes as long as one that has. No password matches
// it.
const dummyPasswordHash = "$argon2id$v=19$m=65536,t=1,p=4$Z29hbC10cmFja2VyLWR1bQ$bm8gcGFzc3dvcmQgZXZlciBoYXNoZXMgdG8gdGhpcy4"

// PasswordLinkURL is the set-password link for token, at the web app's base
// URL.
func (s *Service) PasswordLinkURL(token string) string {
	return s.baseURL + "/set-password?token=" + token
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
