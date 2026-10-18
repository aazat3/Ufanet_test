package main

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

func (a *App) adminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.store.listUsers(r.Context())
	if err != nil {
		a.internalError(w, "list users failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Active   *bool  `json:"active"`
}

func (a *App) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, a.maxBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	user, err := a.store.createUser(r.Context(), strings.TrimSpace(req.Username), req.Password, req.Role, active)
	if err != nil {
		mapStoreError(w, err)
		if !isExpectedStoreError(err) {
			a.logger.Error("create user failed", "error", err)
		}
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

type updateUserRequest struct {
	Active *bool   `json:"active"`
	Role   *string `json:"role"`
}

func (a *App) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user ID")
		return
	}
	var req updateUserRequest
	if err := decodeJSON(w, r, a.maxBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := a.store.updateUser(r.Context(), id, req.Active, req.Role); err != nil {
		mapStoreError(w, err)
		if !isExpectedStoreError(err) {
			a.logger.Error("update user failed", "user_id", id, "error", err)
		}
		return
	}
	a.auth.InvalidateUser(id)
	a.channelCache.InvalidateAll()
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) adminListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := a.store.listAdminChannels(r.Context())
	if err != nil {
		a.internalError(w, "list channels failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

type createChannelRequest struct {
	Number    int    `json:"number"`
	Name      string `json:"name"`
	StreamURL string `json:"stream_url"`
	Active    *bool  `json:"active"`
}

func (a *App) adminCreateChannel(w http.ResponseWriter, r *http.Request) {
	var req createChannelRequest
	if err := decodeJSON(w, r, a.maxBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	channel, err := a.store.createChannel(r.Context(), req.Number, req.Name, req.StreamURL, active)
	if err != nil {
		mapStoreError(w, err)
		if !isExpectedStoreError(err) {
			a.logger.Error("create channel failed", "error", err)
		}
		return
	}
	a.channelCache.InvalidateAll()
	writeJSON(w, http.StatusCreated, channel)
}

type updateChannelRequest struct {
	Number    *int    `json:"number"`
	Name      *string `json:"name"`
	StreamURL *string `json:"stream_url"`
	Active    *bool   `json:"active"`
}

func (a *App) adminUpdateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid channel ID")
		return
	}
	var req updateChannelRequest
	if err := decodeJSON(w, r, a.maxBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := a.store.updateChannel(r.Context(), id, req.Number, req.Name, req.StreamURL, req.Active); err != nil {
		mapStoreError(w, err)
		if !isExpectedStoreError(err) {
			a.logger.Error("update channel failed", "channel_id", id, "error", err)
		}
		return
	}
	a.channelCache.InvalidateAll()
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) adminListSubscriptions(w http.ResponseWriter, r *http.Request) {
	subscriptions, err := a.store.listSubscriptions(r.Context())
	if err != nil {
		a.internalError(w, "list subscriptions failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subscriptions})
}

type putSubscriptionRequest struct {
	UserID    int64      `json:"user_id"`
	ChannelID int64      `json:"channel_id"`
	Enabled   *bool      `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (a *App) adminPutSubscription(w http.ResponseWriter, r *http.Request) {
	var req putSubscriptionRequest
	if err := decodeJSON(w, r, a.maxBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body; expires_at must be RFC3339 or null")
		return
	}
	if req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	if err := a.store.putSubscription(r.Context(), req.UserID, req.ChannelID, *req.Enabled, req.ExpiresAt); err != nil {
		mapStoreError(w, err)
		if !isExpectedStoreError(err) {
			a.logger.Error("save subscription failed", "user_id", req.UserID, "channel_id", req.ChannelID, "error", err)
		}
		return
	}
	a.channelCache.InvalidateAll()
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) adminDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	userID, err1 := parsePathID(r, "userID")
	channelID, err2 := parsePathID(r, "channelID")
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "invalid user or channel ID")
		return
	}
	if err := a.store.deleteSubscription(r.Context(), userID, channelID); err != nil {
		mapStoreError(w, err)
		if !isExpectedStoreError(err) {
			a.logger.Error("delete subscription failed", "user_id", userID, "channel_id", channelID, "error", err)
		}
		return
	}
	a.channelCache.InvalidateAll()
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) internalError(w http.ResponseWriter, message string, err error) {
	a.logger.Error(message, "error", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func isExpectedStoreError(err error) bool {
	return errors.Is(err, errInvalid) || errors.Is(err, errNotFound) || errors.Is(err, errConflict) || errors.Is(err, errLastAdmin)
}
