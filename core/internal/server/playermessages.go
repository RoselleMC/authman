package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/node"
	"github.com/RoselleMC/authman/internal/playermsg"
)

const playerMessagesSettingKey = "player_messages"

type playerMessagesState struct {
	Messages map[string]string               `json:"messages"`
	Dialogs  map[string]*playermsg.DialogDoc `json:"dialogs"`
}

func (s *Server) playerMessagesStateFromStore(ctx context.Context) playerMessagesState {
	state := playerMessagesState{
		Messages: map[string]string{},
		Dialogs:  map[string]*playermsg.DialogDoc{},
	}
	raw, err := s.store.GetSystemSetting(ctx, playerMessagesSettingKey)
	if err != nil || raw == nil {
		return state
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return state
	}
	var decoded struct {
		Messages map[string]string          `json:"messages"`
		Dialogs  map[string]json.RawMessage `json:"dialogs"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return state
	}
	for key, value := range decoded.Messages {
		if playermsg.KnownKey(key) && strings.TrimSpace(value) != "" {
			state.Messages[key] = value
		}
	}
	for _, screen := range playermsg.Screens() {
		raw, ok := decoded.Dialogs[screen]
		if !ok || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		doc, err := playermsg.NormalizeDialog(screen, raw)
		if err != nil {
			continue
		}
		state.Dialogs[screen] = &doc
	}
	return state
}

func playerMessagesSettingMap(state playerMessagesState) map[string]any {
	messages := map[string]any{}
	for key, value := range state.Messages {
		messages[key] = value
	}
	dialogs := map[string]any{}
	for screen, doc := range state.Dialogs {
		if doc == nil {
			continue
		}
		encoded, err := json.Marshal(doc)
		if err != nil {
			continue
		}
		var generic map[string]any
		if err := json.Unmarshal(encoded, &generic); err != nil {
			continue
		}
		dialogs[screen] = generic
	}
	return map[string]any{
		"messages": messages,
		"dialogs":  dialogs,
	}
}

func playerMessagesData(state playerMessagesState) map[string]any {
	dialogs := map[string]any{}
	for _, screen := range playermsg.Screens() {
		entry := map[string]any{
			"default":  playermsg.DefaultDialog(screen),
			"override": nil,
		}
		if doc := state.Dialogs[screen]; doc != nil {
			entry["override"] = doc
		}
		dialogs[screen] = entry
	}
	return map[string]any{
		"messages": map[string]any{
			"defaults":     playermsg.Defaults(""),
			"overrides":    state.Messages,
			"placeholders": playermsg.Placeholders(),
		},
		"dialogs": dialogs,
	}
}

func (s *Server) handleAdminPlayerMessages(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, playerMessagesData(s.playerMessagesStateFromStore(r.Context())), nil)
}

type playerMessagesUpdateRequest struct {
	Messages map[string]string          `json:"messages"`
	Dialogs  map[string]json.RawMessage `json:"dialogs"`
}

func (s *Server) handleAdminUpdatePlayerMessages(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req playerMessagesUpdateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	state := playerMessagesState{
		Messages: map[string]string{},
		Dialogs:  map[string]*playermsg.DialogDoc{},
	}
	fields := map[string]any{}
	for key, value := range req.Messages {
		if strings.TrimSpace(value) == "" {
			continue
		}
		state.Messages[key] = value
	}
	for key, message := range playermsg.ValidateMessages(state.Messages) {
		fields["messages."+key] = message
	}
	for _, screen := range playermsg.Screens() {
		raw, ok := req.Dialogs[screen]
		if !ok || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		doc, err := playermsg.NormalizeDialog(screen, raw)
		if err != nil {
			fields["dialogs."+screen] = "invalid dialog document"
			continue
		}
		for path, message := range playermsg.ValidateDialog(screen, doc) {
			fields["dialogs."+screen+"."+path] = message
		}
		state.Dialogs[screen] = &doc
	}
	if len(fields) > 0 {
		err := api.NewError(http.StatusBadRequest, "player_messages.invalid", "player message configuration is invalid")
		err.Details = map[string]any{"fields": fields}
		api.WriteError(w, err)
		return
	}
	if err := s.store.SetSystemSetting(r.Context(), playerMessagesSettingKey, playerMessagesSettingMap(state)); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "player_messages.save_failed", err.Error()))
		return
	}
	changed := make([]string, 0, len(state.Messages)+len(state.Dialogs))
	for key := range state.Messages {
		changed = append(changed, key)
	}
	for screen := range state.Dialogs {
		changed = append(changed, "dialog."+screen)
	}
	sort.Strings(changed)
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, "player-messages", "player_messages.update", map[string]any{
		"overridden": changed,
	})
	api.WriteJSON(w, http.StatusOK, playerMessagesData(state), nil)
}

// playerMessagesPayload builds the effective message payload delivered to a
// node through the heartbeat response.
func (s *Server) playerMessagesPayload(ctx context.Context, mode string) map[string]any {
	state := s.playerMessagesStateFromStore(ctx)
	if node.NormalizeKind(mode) == "downstream_velocity" {
		return map[string]any{
			"messages": playermsg.Effective(playermsg.ScopeGate, state.Messages),
		}
	}
	dialogs := map[string]playermsg.DialogDoc{}
	for _, screen := range playermsg.Screens() {
		if doc := state.Dialogs[screen]; doc != nil {
			dialogs[screen] = *doc
		} else {
			dialogs[screen] = playermsg.DefaultDialog(screen)
		}
	}
	return map[string]any{
		"messages": playermsg.Effective(playermsg.ScopeLimbo, state.Messages),
		"dialogs":  dialogs,
	}
}
