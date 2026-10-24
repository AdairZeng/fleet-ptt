package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

type DeviceCredential struct {
	DeviceID   string `json:"device_id"`
	MacID      string `json:"mac_id"`
	Credential string `json:"device_credential"`
}

func credentialHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) RegisterDevice(ctx context.Context, owner *store.User, requestedMAC string, now time.Time) (DeviceCredential, error) {
	if s.Secret == "" {
		return DeviceCredential{}, ErrNoSecret
	}
	deviceID, err := newID()
	if err != nil {
		return DeviceCredential{}, err
	}
	userID := ""
	if owner != nil {
		deviceID, userID = owner.DeviceID, owner.ID
	}
	macID := strings.TrimSpace(requestedMAC)
	if macID == "" {
		if owner != nil {
			if existing, lookupErr := s.Store.DeviceByID(ctx, deviceID); lookupErr == nil {
				macID = existing.MacID
			}
		}
		if macID == "" {
			macID, err = newMACID()
			if err != nil {
				return DeviceCredential{}, err
			}
		}
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return DeviceCredential{}, err
	}
	credential := hex.EncodeToString(random[:])
	if err := s.Store.RegisterDevice(ctx, deviceID, userID, macID, credentialHash(credential), now); err != nil {
		return DeviceCredential{}, err
	}
	return DeviceCredential{DeviceID: deviceID, MacID: macID, Credential: credential}, nil
}

func newMACID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[0] = (b[0] & 0xfc) | 0x02
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5]), nil
}

func (s *Service) DeviceSession(ctx context.Context, nickname, deviceID, credential string, now time.Time) (Session, error) {
	if s.Secret == "" {
		return Session{}, ErrNoSecret
	}
	if !validDeviceID(deviceID) || len(credential) != 64 {
		return Session{}, ErrUnauthorized
	}
	device, err := s.Store.DeviceByID(ctx, deviceID)
	hash := credentialHash(credential)
	if err != nil || subtle.ConstantTimeCompare([]byte(hash), []byte(device.CredentialHash)) != 1 {
		return Session{}, ErrUnauthorized
	}
	nickname = strings.TrimSpace(nickname)
	if (nickname != "" || device.UserID == "") && !validNickname(nickname) {
		return Session{}, ErrInvalidNickname
	}
	id, err := newID()
	if err != nil {
		return Session{}, err
	}
	u, err := s.Store.BindDevice(ctx, deviceID, hash, id, nickname, now)
	if err != nil {
		return Session{}, err
	}
	token, err := sign(s.Secret, u.ID, now)
	if err != nil {
		return Session{}, err
	}
	return Session{User: u, Token: token}, nil
}

// AuthenticateDevice verifies a server-issued device credential without
// creating an application session. Hardware boot operations use this proof.
func (s *Service) AuthenticateDevice(ctx context.Context, deviceID, credential string) (store.Device, error) {
	if s.Secret == "" || !validDeviceID(deviceID) || len(credential) != 64 {
		return store.Device{}, ErrUnauthorized
	}
	device, err := s.Store.DeviceByID(ctx, strings.TrimSpace(deviceID))
	if err != nil {
		return store.Device{}, ErrUnauthorized
	}
	hash := credentialHash(credential)
	if subtle.ConstantTimeCompare([]byte(hash), []byte(device.CredentialHash)) != 1 {
		return store.Device{}, ErrUnauthorized
	}
	return device, nil
}
