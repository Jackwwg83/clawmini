package server

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

var ErrConflict = errors.New("conflict")

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	DisplayName string `json:"displayName"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type UserSummary struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	DisplayName string `json:"displayName"`
	DeviceCount int    `json:"deviceCount"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type UserDetail struct {
	User
	Devices []DeviceSnapshot `json:"devices"`
}

type UpdateUserInput struct {
	DisplayName *string
	Role        *string
}

type UserStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) EnsureSchema() error {
	return ensureSchemaMigrations(s.db, schemaNameUsers, 1, map[int]string{
		1: `
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL,
	display_name TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	CHECK (role IN ('admin','user'))
);
CREATE TABLE IF NOT EXISTS user_devices (
	user_id TEXT NOT NULL,
	device_id TEXT NOT NULL,
	PRIMARY KEY(user_id, device_id),
	FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
	FOREIGN KEY(device_id) REFERENCES devices(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_user_devices_device_id ON user_devices(device_id);
`,
	})
}

func normalizeUserRole(raw string) (string, error) {
	role := strings.ToLower(strings.TrimSpace(raw))
	switch role {
	case RoleAdmin, RoleUser:
		return role, nil
	default:
		return "", fmt.Errorf("invalid role")
	}
}

func (s *UserStore) CountUsers() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users;`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *UserStore) CountAdmins() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = ?;`, RoleAdmin).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *UserStore) EnsureDefaultAdmin() (bool, error) {
	count, err := s.CountUsers()
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	_, err = s.CreateUser("admin", "admin", RoleAdmin, "Administrator")
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *UserStore) CreateUser(username, password, role, displayName string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, fmt.Errorf("username is required")
	}
	if password == "" {
		return User{}, fmt.Errorf("password is required")
	}
	role, err := normalizeUserRole(role)
	if err != nil {
		return User{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = username
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}

	id, err := randomHex(16)
	if err != nil {
		return User{}, err
	}
	now := nowUnix()
	_, err = s.db.Exec(`
INSERT INTO users(id, username, password_hash, role, display_name, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?);
`, id, username, string(hash), role, displayName, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrConflict
		}
		return User{}, err
	}

	return User{
		ID:          id,
		Username:    username,
		Role:        role,
		DisplayName: displayName,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique constraint failed")
}

func (s *UserStore) Authenticate(username, password string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return User{}, ErrNotFound
	}

	var id string
	var dbUsername string
	var passwordHash string
	var role string
	var displayName string
	var createdAt int64
	var updatedAt int64
	if err := s.db.QueryRow(`
SELECT id, username, password_hash, role, display_name, created_at, updated_at
FROM users
WHERE username = ?;
`, username).Scan(&id, &dbUsername, &passwordHash, &role, &displayName, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return User{}, ErrNotFound
	}

	return User{
		ID:          id,
		Username:    dbUsername,
		Role:        role,
		DisplayName: displayName,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func (s *UserStore) GetUserByID(id string) (User, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return User{}, ErrNotFound
	}

	var user User
	if err := s.db.QueryRow(`
SELECT id, username, role, display_name, created_at, updated_at
FROM users
WHERE id = ?;
`, id).Scan(&user.ID, &user.Username, &user.Role, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return user, nil
}

func (s *UserStore) ListUsers() ([]UserSummary, error) {
	rows, err := s.db.Query(`
SELECT u.id, u.username, u.role, u.display_name, u.created_at, u.updated_at, COUNT(ud.device_id)
FROM users u
LEFT JOIN user_devices ud ON ud.user_id = u.id
GROUP BY u.id, u.username, u.role, u.display_name, u.created_at, u.updated_at
ORDER BY u.created_at ASC;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]UserSummary, 0)
	for rows.Next() {
		var user UserSummary
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt, &user.DeviceCount); err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

func (s *UserStore) GetUserDetail(id string) (UserDetail, error) {
	user, err := s.GetUserByID(id)
	if err != nil {
		return UserDetail{}, err
	}
	devices, err := s.ListDevicesByUser(id)
	if err != nil {
		return UserDetail{}, err
	}
	return UserDetail{User: user, Devices: devices}, nil
}

func (s *UserStore) UpdateUser(id string, input UpdateUserInput) (User, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return User{}, ErrNotFound
	}

	updates := []string{"updated_at = ?"}
	args := []interface{}{nowUnix()}

	if input.DisplayName != nil {
		displayName := strings.TrimSpace(*input.DisplayName)
		updates = append(updates, "display_name = ?")
		args = append(args, displayName)
	}

	if input.Role != nil {
		role, err := normalizeUserRole(*input.Role)
		if err != nil {
			return User{}, err
		}
		updates = append(updates, "role = ?")
		args = append(args, role)
	}

	args = append(args, id)
	res, err := s.db.Exec(`UPDATE users SET `+strings.Join(updates, ", ")+` WHERE id = ?;`, args...)
	if err != nil {
		return User{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return User{}, err
	}
	if affected == 0 {
		return User{}, ErrNotFound
	}

	return s.GetUserByID(id)
}

func (s *UserStore) SetPassword(userID, password string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ErrNotFound
	}
	if password == "" {
		return fmt.Errorf("password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?;`, string(hash), nowUnix(), userID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *UserStore) VerifyPassword(userID, password string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, ErrNotFound
	}
	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?;`, userID).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return false, nil
	}
	return true, nil
}

func (s *UserStore) DeleteUser(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}
	res, err := s.db.Exec(`DELETE FROM users WHERE id = ?;`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *UserStore) BindDevice(userID, deviceID string) error {
	userID = strings.TrimSpace(userID)
	deviceID = strings.TrimSpace(deviceID)
	if userID == "" || deviceID == "" {
		return ErrNotFound
	}

	if _, err := s.GetUserByID(userID); err != nil {
		return err
	}
	var found string
	if err := s.db.QueryRow(`SELECT id FROM devices WHERE id = ?;`, deviceID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	_, err := s.db.Exec(`INSERT INTO user_devices(user_id, device_id) VALUES(?, ?);`, userID, deviceID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (s *UserStore) UnbindDevice(userID, deviceID string) error {
	userID = strings.TrimSpace(userID)
	deviceID = strings.TrimSpace(deviceID)
	if userID == "" || deviceID == "" {
		return ErrNotFound
	}
	res, err := s.db.Exec(`DELETE FROM user_devices WHERE user_id = ? AND device_id = ?;`, userID, deviceID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *UserStore) IsDeviceBoundToUser(userID, deviceID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	deviceID = strings.TrimSpace(deviceID)
	if userID == "" || deviceID == "" {
		return false, nil
	}
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM user_devices WHERE user_id = ? AND device_id = ?;`, userID, deviceID).Scan(&one)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *UserStore) ListBoundDeviceIDs(userID string) ([]string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return []string{}, nil
	}
	rows, err := s.db.Query(`SELECT device_id FROM user_devices WHERE user_id = ? ORDER BY device_id ASC;`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *UserStore) FilterBoundDeviceIDs(userID string, deviceIDs []string) (map[string]bool, error) {
	result := make(map[string]bool)
	if len(deviceIDs) == 0 {
		return result, nil
	}

	clean := make([]string, 0, len(deviceIDs))
	seen := map[string]struct{}{}
	for _, id := range deviceIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		clean = append(clean, trimmed)
	}
	if len(clean) == 0 {
		return result, nil
	}

	placeholders := strings.Repeat("?,", len(clean))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]interface{}, 0, len(clean)+1)
	args = append(args, strings.TrimSpace(userID))
	for _, id := range clean {
		args = append(args, id)
	}

	rows, err := s.db.Query(`SELECT device_id FROM user_devices WHERE user_id = ? AND device_id IN (`+placeholders+`);`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

func (s *UserStore) ListDevicesByUser(userID string) ([]DeviceSnapshot, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return []DeviceSnapshot{}, nil
	}

	rows, err := s.db.Query(`
SELECT d.id, d.hostname, d.os, d.arch, d.openclaw_version, d.client_version,
	d.has_openclaw,
	d.created_at, d.updated_at, d.last_seen_at,
	s.cpu_usage, s.mem_total, s.mem_used, s.disk_total, s.disk_used, s.uptime,
	s.openclaw_installed, s.openclaw_version, s.gateway_status, s.update_available, s.channels_json, s.updated_at
FROM devices d
JOIN user_devices ud ON ud.device_id = d.id
LEFT JOIN device_status s ON s.device_id = d.id
WHERE ud.user_id = ?
ORDER BY d.updated_at DESC;
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DeviceSnapshot, 0)
	for rows.Next() {
		snap, err := scanDeviceSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}
