package appserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// RuntimeAuthContext is the desktop auth-mode credential the server accepts on
// runtime/authContext. The client does not persist it and does not log it.
type RuntimeAuthContext struct {
	AuthState   string `json:"authState"`
	AccessToken string `json:"accessToken,omitempty"`
	RealUserID  string `json:"realUserID,omitempty"`
	UserEmail   string `json:"userEmail,omitempty"`
	UserName    string `json:"userName,omitempty"`
	SubUserName string `json:"subUserName,omitempty"`
	ExpiresAtMs *int64 `json:"expiresAtMs,omitempty"`
}

// AccountNotice is an account/login/completed or account/updated notification.
// Params keeps every field the server sent.
type AccountNotice struct {
	Params json.RawMessage
}

// Handlers receives server-initiated messages.
//
// OnServerRequest is invoked for a JSON-RPC request the server sends to the
// client. A nil handler answers with method-not-found. The handler's zero
// result is not an approval. Permission decisions are DesktopService calls the
// application makes itself.
//
// Handlers run outside the read loop and may call the client. They must return.
type Handlers struct {
	OnAccountLoginCompleted func(AccountNotice)
	OnAccountUpdated        func(AccountNotice)
	OnServerRequest         func(method string, params json.RawMessage) (any, error)
}

func validateAuthContext(ctx RuntimeAuthContext) error {
	switch ctx.AuthState {
	case "pending", "authenticated", "logged_out":
	default:
		return errors.New("appserver: authState must be pending, authenticated, or logged_out")
	}
	if err := bounded("accessToken", ctx.AccessToken, 1024*1024); err != nil {
		return err
	}
	for _, pair := range []struct {
		name  string
		value string
	}{
		{"realUserID", ctx.RealUserID},
		{"userEmail", ctx.UserEmail},
		{"userName", ctx.UserName},
		{"subUserName", ctx.SubUserName},
	} {
		if err := bounded(pair.name, pair.value, 16*1024); err != nil {
			return err
		}
	}
	if ctx.ExpiresAtMs != nil && (*ctx.ExpiresAtMs <= 0 || ctx.AuthState != "authenticated") {
		return errors.New("appserver: expiresAtMs is only valid for an authenticated context")
	}
	if ctx.AuthState == "logged_out" && (ctx.AccessToken != "" || ctx.RealUserID != "") {
		return errors.New("appserver: a logged-out auth context cannot carry credentials")
	}
	if ctx.AuthState == "authenticated" && (ctx.AccessToken == "" || ctx.RealUserID == "") {
		return errors.New("appserver: an authenticated auth context requires an access token and user id")
	}
	return nil
}

func bounded(name, value string, max int) error {
	if value == "" {
		return nil
	}
	if stringsTrimSpace(value) == "" || !utf8.ValidString(value) || len(value) > max {
		return fmt.Errorf("appserver: invalid %s", name)
	}
	return nil
}

func stringsTrimSpace(s string) string {
	i := 0
	j := len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
