package main

import "time"

type Channel struct {
	ID         int64      `json:"id"`
	Number     int        `json:"number"`
	Name       string     `json:"name"`
	ValidUntil *time.Time `json:"-"`
}

type PlaybackResponse struct {
	ChannelID int64  `json:"channel_id"`
	URL       string `json:"url"`
}

type ChannelsResponse struct {
	Channels []Channel `json:"channels"`
}

type APIError struct {
	Error string `json:"error"`
}

type Principal struct {
	ID       int64
	Username string
	Role     string
}

type authUser struct {
	Principal
	PasswordHash string
	Active       bool
}

type adminUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Active   bool   `json:"active"`
}

type adminChannel struct {
	ID        int64     `json:"id"`
	Number    int       `json:"number"`
	Name      string    `json:"name"`
	StreamURL string    `json:"stream_url"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type adminSubscription struct {
	UserID       int64      `json:"user_id"`
	Username     string     `json:"username"`
	ChannelID    int64      `json:"channel_id"`
	ChannelName  string     `json:"channel_name"`
	Enabled      bool       `json:"enabled"`
	ExpiresAt    *time.Time `json:"expires_at"`
	AccessActive bool       `json:"access_active"`
}
