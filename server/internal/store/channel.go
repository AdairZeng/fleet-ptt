package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Channel 是 channels 表的一行。id 同时预留给以后的 RTN roomName。
type Channel struct {
	ID        string
	Name      string
	JoinCode  string
	CreatedBy string
	CreatedAt time.Time
}

// CreateChannel 写入频道、创建者的 owner 成员，并把创建者的当前频道切过去。
// 口令已存在时返回 ErrJoinCodeTaken，不会留下半截数据。
func (s *Store) CreateChannel(ctx context.Context, ch Channel, role string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var taken int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM channels WHERE join_code = ?`, ch.JoinCode).Scan(&taken)
	if err == nil {
		return ErrJoinCodeTaken
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO channels (id, name, join_code, created_by, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		ch.ID, ch.Name, ch.JoinCode, ch.CreatedBy, ch.CreatedAt.Unix())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memberships (user_id, channel_id, role)
		VALUES (?, ?, ?)`,
		ch.CreatedBy, ch.ID, role); err != nil {
		return err
	}
	if err := setCurrentChannel(ctx, tx, ch.CreatedBy, ch.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// ChannelByJoinCode 按已规范化的口令精确查找。
func (s *Store) ChannelByJoinCode(ctx context.Context, code string) (Channel, error) {
	const q = `SELECT id, name, join_code, created_by, created_at FROM channels WHERE join_code = ?`
	return scanChannel(s.db.QueryRowContext(ctx, q, code))
}

// ChannelsByName 按展示名精确匹配。同名频道可以有多条。
func (s *Store) ChannelsByName(ctx context.Context, name string) ([]Channel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, join_code, created_by, created_at
		FROM channels
		WHERE name = ?
		ORDER BY created_at, id`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Channel
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

// JoinChannel 把用户加入频道并设为当前频道。
// 已经是成员时保留原来的角色，避免 owner 再次加入被降成 member。
// 频道不存在时返回 ErrNotFound。
func (s *Store) JoinChannel(ctx context.Context, userID, channelID, role string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM channels WHERE id = ?`, channelID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memberships (user_id, channel_id, role)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, channel_id) DO NOTHING`,
		userID, channelID, role); err != nil {
		return "", err
	}
	var got string
	if err := tx.QueryRowContext(ctx, `
		SELECT role FROM memberships WHERE user_id = ? AND channel_id = ?`,
		userID, channelID).Scan(&got); err != nil {
		return "", err
	}
	if err := setCurrentChannel(ctx, tx, userID, channelID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return got, nil
}

// ChannelByID 按主键读取频道。不存在时返回 ErrNotFound。
func (s *Store) ChannelByID(ctx context.Context, id string) (Channel, error) {
	const q = `SELECT id, name, join_code, created_by, created_at FROM channels WHERE id = ?`
	return scanChannel(s.db.QueryRowContext(ctx, q, id))
}

// MemberRole 返回用户在该频道的角色。不是成员时返回 ErrNotFound。
func (s *Store) MemberRole(ctx context.Context, userID, channelID string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx, `
		SELECT role FROM memberships WHERE user_id = ? AND channel_id = ?`,
		userID, channelID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return role, nil
}

// CurrentChannel 返回用户当前频道及其角色。还没有当前频道时返回 ErrNotFound。
func (s *Store) CurrentChannel(ctx context.Context, userID string) (Channel, string, error) {
	const q = `
		SELECT c.id, c.name, c.join_code, c.created_by, c.created_at, m.role
		FROM user_session AS us
		JOIN channels AS c ON c.id = us.current_channel_id
		JOIN memberships AS m ON m.channel_id = c.id AND m.user_id = us.user_id
		WHERE us.user_id = ?`
	var ch Channel
	var created int64
	var role string
	err := s.db.QueryRowContext(ctx, q, userID).Scan(
		&ch.ID, &ch.Name, &ch.JoinCode, &ch.CreatedBy, &created, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return Channel{}, "", ErrNotFound
	}
	if err != nil {
		return Channel{}, "", err
	}
	ch.CreatedAt = time.Unix(created, 0).UTC()
	return ch, role, nil
}

type queryRow interface {
	Scan(dest ...any) error
}

func scanChannel(row queryRow) (Channel, error) {
	var ch Channel
	var created int64
	err := row.Scan(&ch.ID, &ch.Name, &ch.JoinCode, &ch.CreatedBy, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, err
	}
	ch.CreatedAt = time.Unix(created, 0).UTC()
	return ch, nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Member 是频道成员及其在该频道的对讲状态。
// 只有当前频道就是这个频道时，talking 才为 true。
type Member struct {
	UserID      string
	DisplayName string
	Talking     bool
}

// SetTalking 更新 user_session.talking。还没有会话行时返回 ErrNotFound。
func (s *Store) SetTalking(ctx context.Context, userID string, talking bool) error {
	bit := 0
	if talking {
		bit = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE user_session SET talking = ? WHERE user_id = ?`, bit, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// IsTalking 读取该用户是否正在对讲。还没有会话行时返回 ErrNotFound。
func (s *Store) IsTalking(ctx context.Context, userID string) (bool, error) {
	var talking int
	err := s.db.QueryRowContext(ctx, `SELECT talking FROM user_session WHERE user_id = ?`, userID).Scan(&talking)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return talking != 0, nil
}

// ChannelMembers 返回频道成员。talking 只在「正在对讲且当前频道就是此频道」时为 true。
func (s *Store) ChannelMembers(ctx context.Context, channelID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.display_name,
			CASE WHEN us.talking = 1 AND us.current_channel_id = m.channel_id THEN 1 ELSE 0 END
		FROM memberships AS m
		JOIN users AS u ON u.id = m.user_id
		LEFT JOIN user_session AS us ON us.user_id = m.user_id
		WHERE m.channel_id = ?
		ORDER BY u.display_name, u.id`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Member{}
	for rows.Next() {
		var member Member
		var talking int
		if err := rows.Scan(&member.UserID, &member.DisplayName, &talking); err != nil {
			return nil, err
		}
		member.Talking = talking != 0
		out = append(out, member)
	}
	return out, rows.Err()
}

func setCurrentChannel(ctx context.Context, ex execer, userID, channelID string) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO user_session (user_id, current_channel_id, talking)
		VALUES (?, ?, 0)
		ON CONFLICT(user_id) DO UPDATE SET current_channel_id = excluded.current_channel_id`,
		userID, channelID)
	return err
}
