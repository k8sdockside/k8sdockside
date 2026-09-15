package services

import (
	"context"

	"github.com/rogerwesterbo/k8sdockside/internal/session"
)

// SessionInfo is which version of the app the window is running in, and for
// whom.
type SessionInfo struct {
	// Server is true in the web version, where the features that need the
	// user's own machine -- file pickers, port forwards, an external terminal,
	// update checks -- are not offered.
	Server bool `json:"server"`
	// Username and Name are who is signed in. Empty in the desktop app.
	Username string `json:"username"`
	Name     string `json:"name"`
	// Admin says whether the user may manage what everyone shares: clusters,
	// plugins, themes, sign-in. Always true in the desktop app, whose one user
	// owns everything.
	Admin bool `json:"admin"`
	// The web version's own pages, empty in the desktop app.
	AccountURL string `json:"accountUrl"`
	AdminURL   string `json:"adminUrl"`
	LogoutURL  string `json:"logoutUrl"`
}

// SessionService tells the frontend which version of the app it is, and who
// is using it, so it can leave out what this version cannot do.
type SessionService struct {
	server bool
}

// Info reports the version and the signed-in user.
func (s *SessionService) Info(ctx context.Context) SessionInfo {
	if !s.server {
		return SessionInfo{Admin: true}
	}
	info := SessionInfo{
		Server:     true,
		AccountURL: session.AccountPath,
		AdminURL:   session.AdminPath,
		LogoutURL:  session.LogoutPath,
	}
	if u, ok := session.FromContext(ctx); ok {
		info.Username, info.Name, info.Admin = u.Username, u.Name, u.Admin
	}
	return info
}
