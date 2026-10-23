package store

import (
	"context"
	"time"
)

// RegisterDevice creates a server-owned identity or migrates a proven legacy user.
func (s *Store) RegisterDevice(ctx context.Context, deviceID, userID, hash string, now time.Time) error {
	if userID == "" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO devices (device_id, user_id, credential_hash, bound_at, last_connected) VALUES (?, '', ?, 0, ?)`, deviceID, hash, now.Unix())
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices (device_id, user_id, credential_hash, bound_at, last_connected)
 VALUES (?, ?, ?, ?, ?) ON CONFLICT(device_id) DO UPDATE SET credential_hash = excluded.credential_hash
 WHERE devices.user_id = excluded.user_id`, deviceID, userID, hash, now.Unix(), now.Unix())
	return err
}

// BindDevice rechecks the credential inside the transaction to prevent concurrent
// first setup from attaching more than one user to the device.
func (s *Store) BindDevice(ctx context.Context, deviceID, hash, newUserID, nickname string, now time.Time) (User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var userID string
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM devices WHERE device_id = ? AND credential_hash = ?`, deviceID, hash).Scan(&userID); err != nil {
		return User{}, ErrNotFound
	}
	if userID == "" {
		userID = newUserID
		if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, display_name, device_id, created_at) VALUES (?, ?, ?, ?)`, userID, nickname, deviceID, now.Unix()); err != nil {
			return User{}, err
		}
	} else if nickname != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET display_name = ? WHERE id = ?`, nickname, userID); err != nil {
			return User{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET user_id = ?, bound_at = CASE WHEN bound_at = 0 THEN ? ELSE bound_at END, last_connected = ? WHERE device_id = ?`, userID, now.Unix(), now.Unix(), deviceID); err != nil {
		return User{}, err
	}
	u, err := scanUser(tx.QueryRowContext(ctx, `SELECT id, display_name, device_id, created_at FROM users WHERE id = ?`, userID))
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

// UserByLinxDeviceID only resolves explicitly provisioned server-side mappings.
func (s *Store) UserByLinxDeviceID(ctx context.Context, id string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT u.id, u.display_name, u.device_id, u.created_at FROM users u JOIN devices d ON d.user_id = u.id WHERE d.linx_device_id = ? AND d.linx_device_id <> ''`, id))
}

// BindLinxDevice is a server-side provisioning operation, never a browser field.
func (s *Store) BindLinxDevice(ctx context.Context, deviceID, linxID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET linx_device_id = ? WHERE device_id = ? AND user_id <> ''`, linxID, deviceID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RenameUser(ctx context.Context, id, nickname string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET display_name = ? WHERE id = ?`, nickname, id)
	return err
}

func (s *Store) PreviousChannel(ctx context.Context, userID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT channel_id FROM device_channel_history WHERE user_id = ?`, userID).Scan(&id)
	return id, err
}
