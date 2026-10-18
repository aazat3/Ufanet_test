package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

//go:embed schema.sql
var schemaFS embed.FS

var (
	errNotFound        = errors.New("not found")
	errConflict        = errors.New("conflict")
	errInvalid         = errors.New("invalid input")
	errUnauthenticated = errors.New("unauthenticated")
	errLastAdmin       = errors.New("cannot deactivate the last active administrator")
)

type Store struct{ db *pgxpool.Pool }

func newStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) initialize(ctx context.Context) error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	for _, statement := range strings.Split(string(schema), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := s.db.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply schema statement: %w", err)
		}
	}

	users := []struct{ username, password, role string }{
		{"admin", "admin12345", "admin"},
		{"viewer", "viewer12345", "user"},
	}
	for _, user := range users {
		hash, err := bcrypt.GenerateFromPassword([]byte(user.password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash user password: %w", err)
		}
		_, err = s.db.Exec(ctx, `INSERT INTO users (username, password_hash, role, active)
			VALUES ($1, $2, $3, TRUE) ON CONFLICT (username) DO NOTHING`, user.username, string(hash), user.role)
		if err != nil {
			return fmt.Errorf("seed user: %w", err)
		}
	}

	channels := []struct {
		number    int
		name, url string
	}{
		{1, "Первый", "https://stream.example.com/live/ch1/index.m3u8"},
		{2, "Россия 1", "https://stream.example.com/live/ch2/index.m3u8"},
		{3, "Матч ТВ", "https://stream.example.com/live/ch3/index.m3u8"},
		{4, "НТВ", "https://stream.example.com/live/ch4/index.m3u8"},
		{5, "Культура", "https://stream.example.com/live/ch5/index.m3u8"},
	}
	for _, channel := range channels {
		_, err := s.db.Exec(ctx, `INSERT INTO channels (number, name, stream_url, active)
			VALUES ($1, $2, $3, TRUE) ON CONFLICT (number) DO NOTHING`, channel.number, channel.name, channel.url)
		if err != nil {
			return fmt.Errorf("seed channel: %w", err)
		}
	}
	_, err = s.db.Exec(ctx, `INSERT INTO subscriptions (user_id, channel_id, enabled, expires_at)
		SELECT u.id, c.id, TRUE, NULL FROM users u CROSS JOIN channels c
		WHERE u.username IN ('admin', 'viewer') 
		AND c.id <= 3
		ON CONFLICT (user_id, channel_id) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("seed subscriptions: %w", err)
	}
	return nil
}

func (s *Store) getAuthUser(ctx context.Context, username string) (authUser, error) {
	var u authUser
	err := s.db.QueryRow(ctx, `SELECT id, username, password_hash, role, active FROM users WHERE username = $1`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return authUser{}, errUnauthenticated
	}
	if err != nil {
		return authUser{}, err
	}
	u.Principal = Principal{ID: u.ID, Username: u.Username, Role: u.Role}
	return u, nil
}

func (s *Store) listChannels(ctx context.Context, userID int64) ([]Channel, error) {
	rows, err := s.db.Query(ctx, `SELECT c.id, c.number, c.name, sub.expires_at
		FROM channels c JOIN subscriptions sub ON sub.channel_id = c.id
		WHERE sub.user_id = $1 AND c.active = TRUE AND sub.enabled = TRUE
		AND (sub.expires_at IS NULL OR sub.expires_at > now())
		ORDER BY c.number`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	channels := make([]Channel, 0)
	for rows.Next() {
		var c Channel
		var expiresAt pgtype.Timestamptz
		if err := rows.Scan(&c.ID, &c.Number, &c.Name, &expiresAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			value := expiresAt.Time
			c.ValidUntil = &value
		}
		channels = append(channels, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return channels, nil
}

func (s *Store) playback(ctx context.Context, userID, channelID int64) (PlaybackResponse, error) {
	var result PlaybackResponse
	err := s.db.QueryRow(ctx, `SELECT c.id, c.stream_url
		FROM channels c JOIN subscriptions sub ON sub.channel_id = c.id
		WHERE c.id = $1 AND c.active = TRUE AND sub.user_id = $2
		AND sub.enabled = TRUE AND (sub.expires_at IS NULL OR sub.expires_at > now())`, channelID, userID).
		Scan(&result.ChannelID, &result.URL)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlaybackResponse{}, errNotFound
	}
	if err != nil {
		return PlaybackResponse{}, err
	}
	return result, nil
}

func (s *Store) listUsers(ctx context.Context) ([]adminUser, error) {
	rows, err := s.db.Query(ctx, `SELECT id, username, role, active FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]adminUser, 0)
	for rows.Next() {
		var u adminUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.Active); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) createUser(ctx context.Context, username, password, role string, active bool) (adminUser, error) {
	if !validUsername(username) || len(password) < 10 || len(password) > 72 || (role != "user" && role != "admin") {
		return adminUser{}, errInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return adminUser{}, err
	}
	var u adminUser
	err = s.db.QueryRow(ctx, `INSERT INTO users (username, password_hash, role, active)
		VALUES ($1, $2, $3, $4) RETURNING id, username, role, active`, username, string(hash), role, active).
		Scan(&u.ID, &u.Username, &u.Role, &u.Active)
	if isUniqueViolation(err) {
		return adminUser{}, errConflict
	}
	return u, err
}

func (s *Store) updateUser(ctx context.Context, id int64, active *bool, role *string) error {
	if id <= 0 || (active == nil && role == nil) {
		return errInvalid
	}
	if role != nil && *role != "user" && *role != "admin" {
		return errInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1)`); err != nil {
		return err
	}
	var oldRole string
	var oldActive bool
	err = tx.QueryRow(ctx, `SELECT role, active FROM users WHERE id = $1 FOR UPDATE`, id).Scan(&oldRole, &oldActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	newRole, newActive := oldRole, oldActive
	if role != nil {
		newRole = *role
	}
	if active != nil {
		newActive = *active
	}
	if oldRole == "admin" && oldActive && (newRole != "admin" || !newActive) {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin' AND active=TRUE`).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return errLastAdmin
		}
	}
	_, err = tx.Exec(ctx, `UPDATE users SET active=$2, role=$3 WHERE id=$1`, id, newActive, newRole)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) listAdminChannels(ctx context.Context) ([]adminChannel, error) {
	rows, err := s.db.Query(ctx, `SELECT id, number, name, stream_url, active, created_at FROM channels ORDER BY number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]adminChannel, 0)
	for rows.Next() {
		var c adminChannel
		if err := rows.Scan(&c.ID, &c.Number, &c.Name, &c.StreamURL, &c.Active, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) createChannel(ctx context.Context, number int, name, streamURL string, active bool) (adminChannel, error) {
	if number <= 0 || strings.TrimSpace(name) == "" || len(name) > 200 || !validStreamURL(streamURL) {
		return adminChannel{}, errInvalid
	}
	var c adminChannel
	err := s.db.QueryRow(ctx, `INSERT INTO channels (number, name, stream_url, active)
		VALUES ($1,$2,$3,$4) RETURNING id, number, name, stream_url, active, created_at`, number, strings.TrimSpace(name), streamURL, active).
		Scan(&c.ID, &c.Number, &c.Name, &c.StreamURL, &c.Active, &c.CreatedAt)
	if isUniqueViolation(err) {
		return adminChannel{}, errConflict
	}
	return c, err
}

func (s *Store) updateChannel(ctx context.Context, id int64, number *int, name, streamURL *string, active *bool) error {
	if id <= 0 || (number == nil && name == nil && streamURL == nil && active == nil) {
		return errInvalid
	}
	if number != nil && *number <= 0 {
		return errInvalid
	}
	if name != nil && (strings.TrimSpace(*name) == "" || len(*name) > 200) {
		return errInvalid
	}
	if streamURL != nil && !validStreamURL(*streamURL) {
		return errInvalid
	}
	_, err := s.db.Exec(ctx, `UPDATE channels SET
		number=COALESCE($2, number), name=COALESCE($3, name),
		stream_url=COALESCE($4, stream_url), active=COALESCE($5, active) WHERE id=$1`,
		id, number, trimPointer(name), streamURL, active)
	if isUniqueViolation(err) {
		return errConflict
	}
	if err != nil {
		return err
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channels WHERE id=$1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errNotFound
	}
	return nil
}

func (s *Store) listSubscriptions(ctx context.Context) ([]adminSubscription, error) {
	rows, err := s.db.Query(ctx, `SELECT u.id, u.username, c.id, c.name, sub.enabled, sub.expires_at,
		u.active AND sub.enabled AND c.active AND (sub.expires_at IS NULL OR sub.expires_at > now())
		FROM subscriptions sub JOIN users u ON u.id=sub.user_id JOIN channels c ON c.id=sub.channel_id
		ORDER BY u.id, c.number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]adminSubscription, 0)
	for rows.Next() {
		var item adminSubscription
		var expiresAt pgtype.Timestamptz
		if err := rows.Scan(&item.UserID, &item.Username, &item.ChannelID, &item.ChannelName, &item.Enabled, &expiresAt, &item.AccessActive); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			value := expiresAt.Time
			item.ExpiresAt = &value
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) putSubscription(ctx context.Context, userID, channelID int64, enabled bool, expiresAt *time.Time) error {
	if userID <= 0 || channelID <= 0 {
		return errInvalid
	}
	_, err := s.db.Exec(ctx, `INSERT INTO subscriptions (user_id, channel_id, enabled, expires_at)
		VALUES ($1,$2,$3,$4) ON CONFLICT (user_id, channel_id)
		DO UPDATE SET enabled=EXCLUDED.enabled, expires_at=EXCLUDED.expires_at`, userID, channelID, enabled, expiresAt)
	if err != nil {
		if isForeignKeyViolation(err) {
			return errNotFound
		}
		return err
	}
	return nil
}

func (s *Store) deleteSubscription(ctx context.Context, userID, channelID int64) error {
	if userID <= 0 || channelID <= 0 {
		return errInvalid
	}
	result, err := s.db.Exec(ctx, `DELETE FROM subscriptions WHERE user_id=$1 AND channel_id=$2`, userID, channelID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errNotFound
	}
	return nil
}

func validUsername(value string) bool {
	if len(value) < 3 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func validStreamURL(value string) bool {
	u, err := url.ParseRequestURI(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func trimPointer(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
