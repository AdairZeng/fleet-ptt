// Package store 用 SQLite 保存用户、频道与成员，并以 device_id 作为用户身份键。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound 表示没有匹配的用户、频道或当前频道。
var ErrNotFound = errors.New("not found")

// ErrJoinCodeTaken 表示口令已被其他频道占用。
var ErrJoinCodeTaken = errors.New("join code taken")

// User 是 users 表的一行。
type User struct {
	ID          string
	DisplayName string
	DeviceID    string
	CreatedAt   time.Time
}

type Device struct {
	DeviceID      string
	UserID        string
	LinxDeviceID  string
	BoundAt       time.Time
	LastConnected time.Time
}

// Store 是单个 SQLite 连接。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）数据库并迁移表结构。
// 表对应技术方案 §4.1：users、channels、memberships、user_session。
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("sqlite path is empty")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite dir: %w", err)
		}
	}
	// busy_timeout：单实例里偶发并发读时等待锁，而不是立刻 SQLITE_BUSY。
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库。
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	// 表结构对应技术方案 §4.1。user_session 记录当前频道，以及该用户是否正在对讲。
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			device_id TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS channels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			join_code TEXT NOT NULL UNIQUE,
			created_by TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS memberships (
			user_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			role TEXT NOT NULL,
			PRIMARY KEY (user_id, channel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS user_session (
			user_id TEXT PRIMARY KEY,
			current_channel_id TEXT,
			talking INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS devices (
			device_id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			linx_device_id TEXT NOT NULL DEFAULT '',
			bound_at INTEGER NOT NULL,
			last_connected INTEGER NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) UpsertDevice(ctx context.Context, deviceID, userID, linxDeviceID string, now time.Time) (Device, error) {
	const q = `INSERT INTO devices (device_id, user_id, linx_device_id, bound_at, last_connected)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET user_id = excluded.user_id,
		linx_device_id = CASE WHEN excluded.linx_device_id <> '' THEN excluded.linx_device_id ELSE devices.linx_device_id END,
		last_connected = excluded.last_connected
		RETURNING device_id, user_id, linx_device_id, bound_at, last_connected`
	return scanDevice(s.db.QueryRowContext(ctx, q, deviceID, userID, linxDeviceID, now.Unix(), now.Unix()))
}

func (s *Store) DeviceByID(ctx context.Context, deviceID string) (Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT device_id, user_id, linx_device_id, bound_at, last_connected FROM devices WHERE device_id = ?`, deviceID))
}

func scanDevice(row *sql.Row) (Device, error) {
	var d Device
	var bound, connected int64
	err := row.Scan(&d.DeviceID, &d.UserID, &d.LinxDeviceID, &bound, &connected)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, err
	}
	d.BoundAt = time.Unix(bound, 0).UTC()
	d.LastConnected = time.Unix(connected, 0).UTC()
	return d, nil
}

// UpsertUser 按 device_id 绑定用户。
// 同一 device_id 再次进入只更新昵称，user id 与 created_at 保持不变。
// 昵称不是身份键：不同设备使用相同昵称会得到不同用户。
func (s *Store) UpsertUser(ctx context.Context, id, displayName, deviceID string, createdAt time.Time) (User, error) {
	const q = `
		INSERT INTO users (id, display_name, device_id, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET display_name = excluded.display_name
		RETURNING id, display_name, device_id, created_at`
	row := s.db.QueryRowContext(ctx, q, id, displayName, deviceID, createdAt.Unix())
	return scanUser(row)
}

// UserByDeviceID 按设备查询用户，供后续灵矽指令把 device_id 解析到人。
func (s *Store) UserByDeviceID(ctx context.Context, deviceID string) (User, error) {
	const q = `SELECT id, display_name, device_id, created_at FROM users WHERE device_id = ?`
	return scanUser(s.db.QueryRowContext(ctx, q, deviceID))
}

// UserByID 按用户 ID 查询。
func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	const q = `SELECT id, display_name, device_id, created_at FROM users WHERE id = ?`
	return scanUser(s.db.QueryRowContext(ctx, q, id))
}

func scanUser(row *sql.Row) (User, error) {
	var u User
	var created int64
	err := row.Scan(&u.ID, &u.DisplayName, &u.DeviceID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	return u, nil
}
