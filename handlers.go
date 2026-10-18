package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

type App struct {
	store        *Store
	auth         *Authenticator
	channelCache *ChannelCache
	logger       *slog.Logger
	maxBodyBytes int64
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("GET /api/v1/channels", a.auth.Middleware(a.logger, false, http.HandlerFunc(a.listChannels)))
	mux.Handle("GET /api/v1/channels/{id}/playback", a.auth.Middleware(a.logger, false, http.HandlerFunc(a.playback)))

	mux.Handle("GET /admin/api/v1/users", a.auth.Middleware(a.logger, true, http.HandlerFunc(a.adminListUsers)))
	mux.Handle("POST /admin/api/v1/users", a.auth.Middleware(a.logger, true, http.HandlerFunc(a.adminCreateUser)))
	mux.Handle("PATCH /admin/api/v1/users/{id}", a.auth.Middleware(a.logger, true, http.HandlerFunc(a.adminUpdateUser)))
	mux.Handle("GET /admin/api/v1/channels", a.auth.Middleware(a.logger, true, http.HandlerFunc(a.adminListChannels)))
	mux.Handle("POST /admin/api/v1/channels", a.auth.Middleware(a.logger, true, http.HandlerFunc(a.adminCreateChannel)))
	mux.Handle("PATCH /admin/api/v1/channels/{id}", a.auth.Middleware(a.logger, true, http.HandlerFunc(a.adminUpdateChannel)))
	mux.Handle("GET /admin/api/v1/subscriptions", a.auth.Middleware(a.logger, true, http.HandlerFunc(a.adminListSubscriptions)))
	mux.Handle("PUT /admin/api/v1/subscriptions", a.auth.Middleware(a.logger, true, http.HandlerFunc(a.adminPutSubscription)))
	mux.Handle("DELETE /admin/api/v1/subscriptions/{userID}/{channelID}", a.auth.Middleware(a.logger, true, http.HandlerFunc(a.adminDeleteSubscription)))

	return recoveryMiddleware(a.logger, loggingMiddleware(a.logger, mux))
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.db.Ping(ctx); err != nil {
		a.logger.Error("health check database ping failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) listChannels(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeAuthRequired(w)
		return
	}
	channels, err := a.channelCache.GetOrLoad(r.Context(), principal.ID, func(ctx context.Context) ([]Channel, error) {
		return a.store.listChannels(ctx, principal.ID)
	})
	if err != nil {
		a.logger.Error("list channels failed", "user_id", principal.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, ChannelsResponse{Channels: channels})
}

func (a *App) playback(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeAuthRequired(w)
		return
	}
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid channel ID")
		return
	}
	result, err := a.store.playback(r.Context(), principal.ID, id)
	if err != nil {
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, "channel not found or unavailable")
			return
		}
		a.logger.Error("get playback failed", "user_id", principal.ID, "channel_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
