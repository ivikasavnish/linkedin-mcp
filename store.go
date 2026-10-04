package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store keeps hosted-mode state in SQLite. LinkedIn tokens are AES-GCM encrypted;
// API keys, OAuth codes and tokens are stored only as SHA-256 hashes.
type Store struct {
	db   *sql.DB
	aead cipher.AEAD
}

type User struct {
	ID        string // LinkedIn OpenID "sub"
	Name      string
	Email     string
	LiToken   string
	LiExpires time.Time
	SubID     string
	SubStatus string
	PaidUntil time.Time
	KeyPrefix string // first chars of current API key, for display
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY, name TEXT, email TEXT,
	li_token BLOB, li_expires INTEGER,
	sub_id TEXT, sub_status TEXT, paid_until INTEGER DEFAULT 0,
	created INTEGER);
CREATE INDEX IF NOT EXISTS users_sub ON users(sub_id);
CREATE TABLE IF NOT EXISTS api_keys (hash TEXT PRIMARY KEY, user_id TEXT UNIQUE, prefix TEXT, created INTEGER);
CREATE TABLE IF NOT EXISTS clients (id TEXT PRIMARY KEY, name TEXT, redirect_uris TEXT, created INTEGER);
CREATE TABLE IF NOT EXISTS codes (hash TEXT PRIMARY KEY, client_id TEXT, user_id TEXT, redirect_uri TEXT,
	challenge TEXT, expires INTEGER);
CREATE TABLE IF NOT EXISTS tokens (hash TEXT PRIMARY KEY, kind TEXT, client_id TEXT, user_id TEXT, expires INTEGER);
CREATE INDEX IF NOT EXISTS tokens_user ON tokens(user_id);
`

func openStore(path string, masterKey []byte) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // sqlite: one writer, avoids SQLITE_BUSY
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	blk, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, aead: aead}, nil
}

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randToken(prefix string) string {
	b := make([]byte, 32)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *Store) seal(plain string) []byte {
	nonce := make([]byte, s.aead.NonceSize())
	rand.Read(nonce)
	return s.aead.Seal(nonce, nonce, []byte(plain), nil)
}

func (s *Store) open(ct []byte) (string, error) {
	n := s.aead.NonceSize()
	if len(ct) < n {
		return "", errors.New("bad ciphertext")
	}
	p, err := s.aead.Open(nil, ct[:n], ct[n:], nil)
	return string(p), err
}

// UpsertUser saves profile + fresh LinkedIn token after sign-in.
func (s *Store) UpsertUser(id, name, email, liToken string, liExpires time.Time) error {
	_, err := s.db.Exec(`INSERT INTO users (id, name, email, li_token, li_expires, created) VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, email=excluded.email,
		li_token=excluded.li_token, li_expires=excluded.li_expires`,
		id, name, email, s.seal(liToken), liExpires.Unix(), time.Now().Unix())
	return err
}

func (s *Store) User(id string) (*User, error) {
	var u User
	var tok []byte
	var exp, paid int64
	var subID, subStatus, prefix sql.NullString
	err := s.db.QueryRow(`SELECT u.id, u.name, u.email, u.li_token, u.li_expires, u.sub_id, u.sub_status, u.paid_until, k.prefix
		FROM users u LEFT JOIN api_keys k ON k.user_id = u.id WHERE u.id = ?`, id).
		Scan(&u.ID, &u.Name, &u.Email, &tok, &exp, &subID, &subStatus, &paid, &prefix)
	if err != nil {
		return nil, err
	}
	if u.LiToken, err = s.open(tok); err != nil {
		return nil, fmt.Errorf("decrypt token: %w", err)
	}
	u.LiExpires, u.PaidUntil = time.Unix(exp, 0), time.Unix(paid, 0)
	u.SubID, u.SubStatus, u.KeyPrefix = subID.String, subStatus.String, prefix.String
	return &u, nil
}

func (s *Store) UserIDBySub(subID string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM users WHERE sub_id = ?`, subID).Scan(&id)
	return id, err
}

func (s *Store) SetSub(userID, subID, status string, paidUntil time.Time) error {
	q := `UPDATE users SET sub_id=?, sub_status=? WHERE id=?`
	args := []any{subID, status, userID}
	if !paidUntil.IsZero() { // never shorten paid time on a later, unrelated event
		q = `UPDATE users SET sub_id=?, sub_status=?, paid_until=MAX(paid_until, ?) WHERE id=?`
		args = []any{subID, status, paidUntil.Unix(), userID}
	}
	_, err := s.db.Exec(q, args...)
	return err
}

func (s *Store) DeleteUser(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM api_keys WHERE user_id=?`, `DELETE FROM tokens WHERE user_id=?`,
		`DELETE FROM codes WHERE user_id=?`, `DELETE FROM users WHERE id=?`} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// NewAPIKey replaces the user's API key and returns the new plaintext key (shown once).
func (s *Store) NewAPIKey(userID string) (string, error) {
	key := randToken("lmk_")
	_, err := s.db.Exec(`INSERT INTO api_keys (hash, user_id, prefix, created) VALUES (?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET hash=excluded.hash, prefix=excluded.prefix, created=excluded.created`,
		hashOf(key), userID, key[:10], time.Now().Unix())
	return key, err
}

func (s *Store) UserByAPIKey(key string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT user_id FROM api_keys WHERE hash=?`, hashOf(key)).Scan(&id)
	return id, err
}

// --- OAuth (MCP authorization server) ---

type OAuthClient struct {
	ID           string
	Name         string
	RedirectURIs string // JSON array
}

func (s *Store) AddClient(c OAuthClient) error {
	_, err := s.db.Exec(`INSERT INTO clients (id, name, redirect_uris, created) VALUES (?,?,?,?)`,
		c.ID, c.Name, c.RedirectURIs, time.Now().Unix())
	return err
}

func (s *Store) Client(id string) (*OAuthClient, error) {
	c := OAuthClient{ID: id}
	err := s.db.QueryRow(`SELECT name, redirect_uris FROM clients WHERE id=?`, id).Scan(&c.Name, &c.RedirectURIs)
	return &c, err
}

type authCode struct {
	ClientID, UserID, RedirectURI, Challenge string
}

func (s *Store) PutCode(code string, c authCode, ttl time.Duration) error {
	_, err := s.db.Exec(`INSERT INTO codes (hash, client_id, user_id, redirect_uri, challenge, expires) VALUES (?,?,?,?,?,?)`,
		hashOf(code), c.ClientID, c.UserID, c.RedirectURI, c.Challenge, time.Now().Add(ttl).Unix())
	return err
}

// TakeCode returns and deletes a code: codes are single use.
func (s *Store) TakeCode(code string) (*authCode, error) {
	var c authCode
	var exp int64
	err := s.db.QueryRow(`DELETE FROM codes WHERE hash=? RETURNING client_id, user_id, redirect_uri, challenge, expires`,
		hashOf(code)).Scan(&c.ClientID, &c.UserID, &c.RedirectURI, &c.Challenge, &exp)
	if err != nil {
		return nil, err
	}
	if time.Now().Unix() > exp {
		return nil, errors.New("code expired")
	}
	return &c, nil
}

func (s *Store) PutToken(tok, kind, clientID, userID string, ttl time.Duration) error {
	_, err := s.db.Exec(`INSERT INTO tokens (hash, kind, client_id, user_id, expires) VALUES (?,?,?,?,?)`,
		hashOf(tok), kind, clientID, userID, time.Now().Add(ttl).Unix())
	return err
}

// Token looks up a token of the given kind; take=true deletes it (refresh rotation).
func (s *Store) Token(tok, kind string, take bool) (clientID, userID string, expires time.Time, err error) {
	q := `SELECT client_id, user_id, expires FROM tokens WHERE hash=? AND kind=?`
	if take {
		q = `DELETE FROM tokens WHERE hash=? AND kind=? RETURNING client_id, user_id, expires`
	}
	var exp int64
	err = s.db.QueryRow(q, hashOf(tok), kind).Scan(&clientID, &userID, &exp)
	expires = time.Unix(exp, 0)
	if err == nil && time.Now().After(expires) {
		err = errors.New("token expired")
	}
	return
}

// Sweep removes expired codes and tokens.
func (s *Store) Sweep() {
	now := time.Now().Unix()
	s.db.Exec(`DELETE FROM codes WHERE expires < ?`, now)
	s.db.Exec(`DELETE FROM tokens WHERE expires < ?`, now)
}

// RevokeTokens disconnects every OAuth app of a user.
func (s *Store) RevokeTokens(userID string) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE user_id=?`, userID)
	return err
}
