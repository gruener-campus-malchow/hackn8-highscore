package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gruener-campus-malchow/hackn8-highscore/internal/models"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func New(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes
	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) migrate() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ticket_code TEXT UNIQUE NOT NULL,
			nickname TEXT NOT NULL DEFAULT '',
			is_admin INTEGER NOT NULL DEFAULT 0,
			total_points INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS activities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			location TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL,
			created_by INTEGER REFERENCES users(id),
			enabled INTEGER NOT NULL DEFAULT 0,
			points INTEGER,
			token TEXT UNIQUE NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS scans (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id),
			activity_id INTEGER NOT NULL REFERENCES activities(id),
			scanned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, activity_id)
		);

		CREATE TABLE IF NOT EXISTS config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS invalidated_tokens (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			token          TEXT NOT NULL,
			activity_id    INTEGER NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
			invalidated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_invalidated_tokens_token ON invalidated_tokens(token);

		INSERT OR IGNORE INTO config (key, value) VALUES
			('gaming_threshold', '1000'),
			('default_workshop_points', '100'),
			('default_hidden_points', '20'),
			('penalty_points', '50'),
			('gaming_threshold_mode', 'absolute'),
			('gaming_threshold_multiplier', '100'),
			('use_pretix_checkin', '0'),
			('require_pretix_login', '1'),
			('ticket_code_regex', '^[A-Z0-9]{5}$');
	`)
	if err != nil {
		return err
	}
	// Add location column to existing DBs (ignored if already present)
	_, err = db.Exec(`ALTER TABLE activities ADD COLUMN location TEXT NOT NULL DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name: location") {
		return err
	}
	// Add hidden_from_leaderboard column to existing DBs (ignored if already present)
	_, err = db.Exec(`ALTER TABLE users ADD COLUMN hidden_from_leaderboard INTEGER NOT NULL DEFAULT 0`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name: hidden_from_leaderboard") {
		return err
	}
	// Add creator_bonus column to existing DBs (ignored if already present)
	_, err = db.Exec(`ALTER TABLE activities ADD COLUMN creator_bonus INTEGER NOT NULL DEFAULT 0`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name: creator_bonus") {
		return err
	}
	// Add creator_bonus_percentage config key if not present
	_, err = db.Exec(`INSERT OR IGNORE INTO config (key, value) VALUES ('creator_bonus_percentage', '10')`)
	if err != nil {
		return err
	}
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// User operations

// FindUsersByTicketPrefix returns all users whose ticket_code equals prefix or
// matches the pattern "prefix-N". Used to detect returning users before Pretix
// validation.
func (db *DB) FindUsersByTicketPrefix(prefix string) ([]*models.User, error) {
	rows, err := db.Query(
		`SELECT id, ticket_code, nickname, is_admin, total_points, hidden_from_leaderboard FROM users WHERE ticket_code = ? OR ticket_code LIKE ?`,
		prefix, prefix+"-%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.TicketCode, &u.Nickname, &u.IsAdmin, &u.TotalPoints, &u.HiddenFromLeaderboard); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

func (db *DB) GetOrCreateUser(ticketCode string) (*models.User, bool, error) {
	var u models.User
	err := db.QueryRow(
		`SELECT id, ticket_code, nickname, is_admin, total_points, hidden_from_leaderboard FROM users WHERE ticket_code = ?`,
		ticketCode,
	).Scan(&u.ID, &u.TicketCode, &u.Nickname, &u.IsAdmin, &u.TotalPoints, &u.HiddenFromLeaderboard)
	if err == sql.ErrNoRows {
		res, err := db.Exec(`INSERT INTO users (ticket_code) VALUES (?)`, ticketCode)
		if err != nil {
			return nil, false, err
		}
		u.ID, _ = res.LastInsertId()
		u.TicketCode = ticketCode
		return &u, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &u, false, nil
}

func (db *DB) GetUserByID(id int64) (*models.User, error) {
	var u models.User
	err := db.QueryRow(
		`SELECT id, ticket_code, nickname, is_admin, total_points, hidden_from_leaderboard FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.TicketCode, &u.Nickname, &u.IsAdmin, &u.TotalPoints, &u.HiddenFromLeaderboard)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &u, err
}

func (db *DB) SetNickname(userID int64, nickname string) error {
	_, err := db.Exec(`UPDATE users SET nickname = ? WHERE id = ?`, nickname, userID)
	return err
}

func (db *DB) SetAdmin(userID int64) error {
	_, err := db.Exec(`UPDATE users SET is_admin = 1 WHERE id = ?`, userID)
	return err
}

func (db *DB) SetAdminStatus(userID int64, isAdmin bool) error {
	v := 0
	if isAdmin {
		v = 1
	}
	_, err := db.Exec(`UPDATE users SET is_admin = ? WHERE id = ?`, v, userID)
	return err
}

func (db *DB) GetAllUsers() ([]*models.User, error) {
	rows, err := db.Query(
		`SELECT id, ticket_code, nickname, is_admin, total_points, hidden_from_leaderboard FROM users ORDER BY total_points DESC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.TicketCode, &u.Nickname, &u.IsAdmin, &u.TotalPoints, &u.HiddenFromLeaderboard); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

// Config operations

func (db *DB) GetConfig() (*models.Config, error) {
	rows, err := db.Query(`SELECT key, value FROM config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cfg := &models.Config{
		GamingThreshold:           1000,
		GamingThresholdMode:       "absolute",
		GamingThresholdMultiplier: 100,
		DefaultWorkshopPoints:     100,
		DefaultHiddenPoints:       20,
		PenaltyPoints:             50,
		RequirePretixLogin:        true,
		TicketCodeRegex:           `^[A-Z0-9]{5}$`,
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		n, _ := strconv.Atoi(v)
		switch k {
		case "gaming_threshold":
			cfg.GamingThreshold = n
		case "gaming_threshold_mode":
			cfg.GamingThresholdMode = v
		case "gaming_threshold_multiplier":
			cfg.GamingThresholdMultiplier = n
		case "default_workshop_points":
			cfg.DefaultWorkshopPoints = n
		case "default_hidden_points":
			cfg.DefaultHiddenPoints = n
		case "penalty_points":
			cfg.PenaltyPoints = n
		case "use_pretix_checkin":
			cfg.UsePretixCheckin = v == "1"
		case "require_pretix_login":
			cfg.RequirePretixLogin = v == "1"
		case "ticket_code_regex":
			if v != "" {
				cfg.TicketCodeRegex = v
			}
		case "creator_bonus_percentage":
			cfg.CreatorBonusPercentage = n
		}
	}
	return cfg, rows.Err()
}

func (db *DB) SetConfig(key, value string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO config (key, value) VALUES (?, ?)`, key, value)
	return err
}

func ComputeEffectiveThreshold(cfg *models.Config, userCount int) int {
	if cfg.GamingThresholdMode == "multiplier" {
		return cfg.GamingThresholdMultiplier * userCount
	}
	return cfg.GamingThreshold
}

// Activity operations

func (db *DB) CreateActivity(name, description, location string, atype models.ActivityType, createdBy int64) (*models.Activity, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	res, err := db.Exec(
		`INSERT INTO activities (name, description, location, type, created_by, token) VALUES (?, ?, ?, ?, ?, ?)`,
		name, description, location, atype, createdBy, token,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &models.Activity{
		ID:          id,
		Name:        name,
		Description: description,
		Location:    location,
		Type:        atype,
		CreatedBy:   createdBy,
		Token:       token,
	}, nil
}

func (db *DB) CreateHiddenActivity(name string, points *int, createdBy int64) (*models.Activity, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	var res sql.Result
	if points != nil {
		res, err = db.Exec(
			`INSERT INTO activities (name, type, created_by, enabled, points, token) VALUES (?, 'hidden', ?, 1, ?, ?)`,
			name, createdBy, *points, token,
		)
	} else {
		res, err = db.Exec(
			`INSERT INTO activities (name, type, created_by, enabled, token) VALUES (?, 'hidden', ?, 1, ?)`,
			name, createdBy, token,
		)
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &models.Activity{ID: id, Name: name, Type: models.ActivityHidden, CreatedBy: createdBy, Enabled: true, Points: points, Token: token}, nil
}

func (db *DB) GetActivityByToken(token string) (*models.Activity, error) {
	var a models.Activity
	err := db.QueryRow(
		`SELECT id, name, description, location, type, created_by, enabled, points, token, creator_bonus FROM activities WHERE token = ?`, token,
	).Scan(&a.ID, &a.Name, &a.Description, &a.Location, &a.Type, &a.CreatedBy, &a.Enabled, &a.Points, &a.Token, &a.CreatorBonus)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &a, err
}

func (db *DB) GetActivityByID(id int64) (*models.Activity, error) {
	var a models.Activity
	err := db.QueryRow(
		`SELECT id, name, description, location, type, created_by, enabled, points, token, creator_bonus FROM activities WHERE id = ?`, id,
	).Scan(&a.ID, &a.Name, &a.Description, &a.Location, &a.Type, &a.CreatedBy, &a.Enabled, &a.Points, &a.Token, &a.CreatorBonus)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &a, err
}

func (db *DB) GetAllActivities() ([]*models.Activity, error) {
	rows, err := db.Query(
		`SELECT id, name, description, location, type, created_by, enabled, points, token, creator_bonus FROM activities ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var activities []*models.Activity
	for rows.Next() {
		var a models.Activity
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Location, &a.Type, &a.CreatedBy, &a.Enabled, &a.Points, &a.Token, &a.CreatorBonus); err != nil {
			return nil, err
		}
		activities = append(activities, &a)
	}
	return activities, rows.Err()
}

func (db *DB) GetUserActivities(userID int64) ([]*models.Activity, error) {
	rows, err := db.Query(
		`SELECT id, name, description, location, type, created_by, enabled, points, token, creator_bonus FROM activities WHERE created_by = ? AND type = 'workshop' ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var activities []*models.Activity
	for rows.Next() {
		var a models.Activity
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Location, &a.Type, &a.CreatedBy, &a.Enabled, &a.Points, &a.Token, &a.CreatorBonus); err != nil {
			return nil, err
		}
		activities = append(activities, &a)
	}
	return activities, rows.Err()
}

func (db *DB) ToggleActivity(id int64) (bool, error) {
	_, err := db.Exec(`UPDATE activities SET enabled = NOT enabled WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	var enabled bool
	err = db.QueryRow(`SELECT enabled FROM activities WHERE id = ?`, id).Scan(&enabled)
	return enabled, err
}

func (db *DB) SetActivityPoints(id int64, points *int) error {
	if points == nil {
		_, err := db.Exec(`UPDATE activities SET points = NULL WHERE id = ?`, id)
		return err
	}
	_, err := db.Exec(`UPDATE activities SET points = ? WHERE id = ?`, *points, id)
	return err
}

// Scan operations

func (db *DB) RecordScan(userID, activityID int64) error {
	_, err := db.Exec(
		`INSERT INTO scans (user_id, activity_id) VALUES (?, ?)`,
		userID, activityID,
	)
	return err
}

func (db *DB) HasScanned(userID, activityID int64) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM scans WHERE user_id = ? AND activity_id = ?`, userID, activityID,
	).Scan(&count)
	return count > 0, err
}

func (db *DB) AddPoints(userID int64, points int) error {
	_, err := db.Exec(`UPDATE users SET total_points = total_points + ? WHERE id = ?`, points, userID)
	return err
}

// Leaderboard

func (db *DB) SetLeaderboardHidden(userID int64, hidden bool) error {
	v := 0
	if hidden {
		v = 1
	}
	_, err := db.Exec(`UPDATE users SET hidden_from_leaderboard = ? WHERE id = ?`, v, userID)
	return err
}

func (db *DB) GetLeaderboard() ([]*models.LeaderboardEntry, error) {
	rows, err := db.Query(`
		SELECT id, nickname, ticket_code, total_points
		FROM users
		WHERE hidden_from_leaderboard = 0
		ORDER BY total_points DESC, id ASC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []*models.LeaderboardEntry
	rank := 1
	for rows.Next() {
		var id int64
		var nickname, ticketCode string
		var points int
		if err := rows.Scan(&id, &nickname, &ticketCode, &points); err != nil {
			return nil, err
		}
		displayName := nickname
		if displayName == "" {
			if len(ticketCode) > 8 {
				displayName = ticketCode[:8] + "..."
			} else {
				displayName = ticketCode
			}
		}
		entries = append(entries, &models.LeaderboardEntry{
			UserID:      id,
			Rank:        rank,
			DisplayName: displayName,
			TotalPoints: points,
		})
		rank++
	}
	return entries, rows.Err()
}

func (db *DB) GetTotalPoints() (int, error) {
	var total int
	err := db.QueryRow(`SELECT COALESCE(SUM(total_points), 0) FROM users`).Scan(&total)
	return total, err
}

func (db *DB) ResolvePoints(a *models.Activity, cfg *models.Config) int {
	if a.Points != nil {
		return *a.Points
	}
	if a.Type == models.ActivityHidden {
		return cfg.DefaultHiddenPoints
	}
	return cfg.DefaultWorkshopPoints
}

func (db *DB) DeleteActivity(id int64) error {
	_, err := db.Exec(`DELETE FROM activities WHERE id = ?`, id)
	return err
}

func (db *DB) ToggleCreatorBonus(id int64) (bool, error) {
	_, err := db.Exec(`UPDATE activities SET creator_bonus = NOT creator_bonus WHERE id = ? AND type = 'workshop'`, id)
	if err != nil {
		return false, err
	}
	var bonus bool
	err = db.QueryRow(`SELECT creator_bonus FROM activities WHERE id = ?`, id).Scan(&bonus)
	return bonus, err
}

func (db *DB) GetUserCount() (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE hidden_from_leaderboard = 0`).Scan(&count)
	return count, err
}

func (db *DB) GetScanCount() (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM scans`).Scan(&count)
	return count, err
}

// ActivityWithCreator for admin display
type ActivityWithCreator struct {
	models.Activity
	CreatorName string
	ScanCount   int
}

func (db *DB) GetAllWorkshopsWithCreators() ([]*ActivityWithCreator, error) {
	rows, err := db.Query(`
		SELECT a.id, a.name, a.description, a.location, a.type, a.created_by, a.enabled, a.points, a.token, a.creator_bonus,
		       COALESCE(NULLIF(u.nickname,''), u.ticket_code) as creator_name,
		       COUNT(s.id) as scan_count
		FROM activities a
		LEFT JOIN users u ON u.id = a.created_by
		LEFT JOIN scans s ON s.activity_id = a.id
		WHERE a.type = 'workshop'
		GROUP BY a.id
		ORDER BY a.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*ActivityWithCreator
	for rows.Next() {
		var ac ActivityWithCreator
		if err := rows.Scan(
			&ac.ID, &ac.Name, &ac.Description, &ac.Location, &ac.Type, &ac.CreatedBy,
			&ac.Enabled, &ac.Points, &ac.Token, &ac.CreatorBonus, &ac.CreatorName, &ac.ScanCount,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		list = append(list, &ac)
	}
	return list, rows.Err()
}

func (db *DB) GetAllActivitiesWithCreators() ([]*ActivityWithCreator, error) {
	rows, err := db.Query(`
		SELECT a.id, a.name, a.description, a.location, a.type, a.created_by, a.enabled, a.points, a.token, a.creator_bonus,
		       COALESCE(NULLIF(u.nickname,''), u.ticket_code) as creator_name,
		       COUNT(s.id) as scan_count
		FROM activities a
		LEFT JOIN users u ON u.id = a.created_by
		LEFT JOIN scans s ON s.activity_id = a.id
		GROUP BY a.id
		ORDER BY a.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*ActivityWithCreator
	for rows.Next() {
		var ac ActivityWithCreator
		if err := rows.Scan(
			&ac.ID, &ac.Name, &ac.Description, &ac.Location, &ac.Type, &ac.CreatedBy,
			&ac.Enabled, &ac.Points, &ac.Token, &ac.CreatorBonus, &ac.CreatorName, &ac.ScanCount,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		list = append(list, &ac)
	}
	return list, rows.Err()
}

// Anti-cheat: token rotation

func (db *DB) RotateWorkshopToken(activityID int64) (string, error) {
	newToken, err := randomToken()
	if err != nil {
		return "", err
	}
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var oldToken string
	if err := tx.QueryRow(`SELECT token FROM activities WHERE id = ?`, activityID).Scan(&oldToken); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO invalidated_tokens (token, activity_id) VALUES (?, ?)`, oldToken, activityID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE activities SET token = ? WHERE id = ?`, newToken, activityID); err != nil {
		return "", err
	}
	return newToken, tx.Commit()
}

func (db *DB) GetActivityByInvalidatedToken(token string) (*models.Activity, error) {
	var a models.Activity
	err := db.QueryRow(`
		SELECT a.id, a.name, a.description, a.location, a.type, a.created_by, a.enabled, a.points, a.token, a.creator_bonus
		FROM invalidated_tokens it
		JOIN activities a ON a.id = it.activity_id
		WHERE it.token = ?
		LIMIT 1
	`, token).Scan(&a.ID, &a.Name, &a.Description, &a.Location, &a.Type, &a.CreatedBy, &a.Enabled, &a.Points, &a.Token, &a.CreatorBonus)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &a, err
}

// UserScanEntry holds a single scan record with resolved activity info and points.
type UserScanEntry struct {
	ActivityName string
	ActivityType models.ActivityType
	Points       int
	ScannedAt    time.Time
}

func (db *DB) GetUserScans(userID int64, cfg *models.Config) ([]*UserScanEntry, error) {
	rows, err := db.Query(`
		SELECT a.name, a.type, a.points, s.scanned_at
		FROM scans s
		JOIN activities a ON a.id = s.activity_id
		WHERE s.user_id = ?
		ORDER BY s.scanned_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []*UserScanEntry
	for rows.Next() {
		var e UserScanEntry
		var pts *int
		var scannedAt string
		if err := rows.Scan(&e.ActivityName, &e.ActivityType, &pts, &scannedAt); err != nil {
			return nil, err
		}
		t, err := parseTime(scannedAt)
		if err != nil {
			return nil, err
		}
		e.ScannedAt = t
		if pts != nil {
			e.Points = *pts
		} else if e.ActivityType == models.ActivityHidden {
			e.Points = cfg.DefaultHiddenPoints
		} else {
			e.Points = cfg.DefaultWorkshopPoints
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

func (db *DB) GetUserRank(userID int64) (int, error) {
	var rank int
	err := db.QueryRow(`
		SELECT COUNT(*) + 1
		FROM users
		WHERE total_points > (SELECT total_points FROM users WHERE id = ?)
		AND hidden_from_leaderboard = 0
	`, userID).Scan(&rank)
	return rank, err
}

func (db *DB) ApplyPenalty(userID int64, penalty int) (int, error) {
	var current int
	if err := db.QueryRow(`SELECT total_points FROM users WHERE id = ?`, userID).Scan(&current); err != nil {
		return 0, err
	}
	actual := penalty
	if current < penalty {
		actual = current
	}
	_, err := db.Exec(`UPDATE users SET total_points = MAX(0, total_points - ?) WHERE id = ?`, penalty, userID)
	return actual, err
}

func (db *DB) GetLastScan(activityID int64) (*models.LastScanInfo, error) {
	var displayName string
	var scannedAt string
	err := db.QueryRow(`
		SELECT COALESCE(NULLIF(u.nickname, ''), u.ticket_code), s.scanned_at
		FROM scans s
		JOIN users u ON u.id = s.user_id
		WHERE s.activity_id = ?
		ORDER BY s.scanned_at DESC
		LIMIT 1
	`, activityID).Scan(&displayName, &scannedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := parseTime(scannedAt)
	if err != nil {
		return nil, err
	}
	return &models.LastScanInfo{UserDisplayName: displayName, ScannedAt: t}, nil
}

var berlinLoc = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic("failed to load Europe/Berlin timezone: " + err.Error())
	}
	return loc
}()

func parseTime(s string) (t time.Time, err error) {
	for _, layout := range []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	} {
		if t, err = time.Parse(layout, s); err == nil {
			return t.In(berlinLoc), nil
		}
	}
	return
}
