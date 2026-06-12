package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/recoverykey"
	"github.com/RoselleMC/authman/core/internal/store"
)

const (
	passwordRecoveryKeySettingKey = "password_recovery_key"
	systemFactoryResetConfirm     = "RESET AUTHMAN"
)

type passwordRecoveryKeyState struct {
	Algorithm     string `json:"algorithm"`
	PublicKeyPEM  string `json:"public_key_pem"`
	PrivateKeyPEM string `json:"private_key_pem,omitempty"`
	Fingerprint   string `json:"fingerprint"`
	SizeBits      int    `json:"size_bits"`
	CreatedAt     string `json:"created_at"`
	DownloadedAt  string `json:"downloaded_at,omitempty"`
}

type systemFactoryResetRequest struct {
	Confirm string `json:"confirm"`
}

func (s *Server) handleAdminPasswordRecoveryKey(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	state, err := s.ensurePasswordRecoveryKey(r.Context())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "password_recovery.key_unavailable", "failed to load password recovery key"))
		return
	}
	api.WriteJSON(w, http.StatusOK, passwordRecoveryKeyData(state), nil)
}

func (s *Server) handleAdminDownloadPasswordRecoveryPrivateKey(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	unlock := s.passportLocks.lock("system:" + passwordRecoveryKeySettingKey)
	defer unlock()
	state, err := s.ensurePasswordRecoveryKeyLocked(r.Context())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "password_recovery.key_unavailable", "failed to load password recovery key"))
		return
	}
	privateKey := strings.TrimSpace(state.PrivateKeyPEM)
	if privateKey == "" {
		api.WriteError(w, api.NewError(http.StatusGone, "password_recovery.private_key_destroyed", "private key has already been downloaded and destroyed"))
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	state.PrivateKeyPEM = ""
	state.DownloadedAt = now
	if err := s.savePasswordRecoveryKey(r.Context(), state); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "password_recovery.key_save_failed", "failed to destroy private key after download"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, passwordRecoveryKeySettingKey, "password_recovery.private_key.download", map[string]any{
		"fingerprint": state.Fingerprint,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"private_key_pem": privateKey + "\n",
		"fingerprint":     state.Fingerprint,
		"algorithm":       state.Algorithm,
		"filename":        fmt.Sprintf("authman-password-recovery-%s.pem", shortFingerprint(state.Fingerprint)),
		"destroyed_at":    now,
	}, nil)
}

func (s *Server) handleAdminSystemFactoryReset(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req systemFactoryResetRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	if strings.TrimSpace(req.Confirm) != systemFactoryResetConfirm {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "system.factory_reset.confirm_required", "confirmation phrase is required"))
		return
	}
	unlock := s.passportLocks.lock("system:" + passwordRecoveryKeySettingKey)
	defer unlock()
	if err := s.store.FactoryResetPlayerData(r.Context()); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.factory_reset_failed", "failed to reset player data"))
		return
	}
	state, err := newPasswordRecoveryKeyState(time.Now().UTC())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "password_recovery.key_generate_failed", "failed to generate password recovery key"))
		return
	}
	if err := s.savePasswordRecoveryKey(r.Context(), state); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "password_recovery.key_save_failed", "failed to save password recovery key"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, "factory_reset", "system.factory_reset", map[string]any{
		"fingerprint":         state.Fingerprint,
		"player_data_reset":   true,
		"private_key_pending": true,
	})
	api.WriteJSON(w, http.StatusOK, passwordRecoveryKeyData(state), nil)
}

func (s *Server) offlinePasswordCredential(ctx context.Context, password string) (string, string, error) {
	state, err := s.ensurePasswordRecoveryKey(ctx)
	if err != nil {
		return "", "", err
	}
	encrypted, fingerprint, err := recoverykey.Encrypt(state.PublicKeyPEM, []byte(password))
	if err != nil {
		return "", "", err
	}
	return encrypted, fingerprint, nil
}

func (s *Server) ensurePasswordRecoveryKey(ctx context.Context) (passwordRecoveryKeyState, error) {
	unlock := s.passportLocks.lock("system:" + passwordRecoveryKeySettingKey)
	defer unlock()
	return s.ensurePasswordRecoveryKeyLocked(ctx)
}

func (s *Server) ensurePasswordRecoveryKeyLocked(ctx context.Context) (passwordRecoveryKeyState, error) {
	state, err := s.loadPasswordRecoveryKey(ctx)
	if err == nil && strings.TrimSpace(state.PublicKeyPEM) != "" && strings.TrimSpace(state.Fingerprint) != "" {
		return state, nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return passwordRecoveryKeyState{}, err
	}
	state, err = newPasswordRecoveryKeyState(time.Now().UTC())
	if err != nil {
		return passwordRecoveryKeyState{}, err
	}
	if err := s.savePasswordRecoveryKey(ctx, state); err != nil {
		return passwordRecoveryKeyState{}, err
	}
	return state, nil
}

func (s *Server) loadPasswordRecoveryKey(ctx context.Context) (passwordRecoveryKeyState, error) {
	raw, err := s.store.GetSystemSetting(ctx, passwordRecoveryKeySettingKey)
	if err != nil {
		return passwordRecoveryKeyState{}, err
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return passwordRecoveryKeyState{}, err
	}
	var state passwordRecoveryKeyState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return passwordRecoveryKeyState{}, err
	}
	return state, nil
}

func (s *Server) savePasswordRecoveryKey(ctx context.Context, state passwordRecoveryKeyState) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return err
	}
	return s.store.SetSystemSetting(ctx, passwordRecoveryKeySettingKey, value)
}

func newPasswordRecoveryKeyState(now time.Time) (passwordRecoveryKeyState, error) {
	pair, err := recoverykey.Generate()
	if err != nil {
		return passwordRecoveryKeyState{}, err
	}
	return passwordRecoveryKeyState{
		Algorithm:     pair.Algorithm,
		PublicKeyPEM:  pair.PublicPEM,
		PrivateKeyPEM: pair.PrivatePEM,
		Fingerprint:   pair.Fingerprint,
		SizeBits:      pair.SizeBits,
		CreatedAt:     now.UTC().Format(time.RFC3339),
	}, nil
}

func passwordRecoveryKeyData(state passwordRecoveryKeyState) map[string]any {
	return map[string]any{
		"algorithm":             state.Algorithm,
		"public_key_pem":        state.PublicKeyPEM,
		"fingerprint":           state.Fingerprint,
		"size_bits":             state.SizeBits,
		"created_at":            state.CreatedAt,
		"downloaded_at":         state.DownloadedAt,
		"private_key_available": strings.TrimSpace(state.PrivateKeyPEM) != "",
	}
}

func shortFingerprint(fingerprint string) string {
	fingerprint = strings.TrimSpace(fingerprint)
	if len(fingerprint) <= 12 {
		return fingerprint
	}
	return fingerprint[:12]
}
