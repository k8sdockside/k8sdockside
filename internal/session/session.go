// Package session carries who is asking through a call, and remembers who
// opened each stream, for the web version of the app.
//
// The desktop app has one user, the person at the machine, and nothing here
// applies to it: a call without a user in its context is a call from the
// desktop window, and every check in this package lets it through. In server
// mode the gateway in front of the app authenticates each request and attaches
// the user; the services use that to refuse administrative calls from anyone
// who is not an administrator, and to claim the streams they open, so that one
// user's terminal output, log lines and table rows reach that user and nobody
// else.
package session

import (
	"context"
	"errors"
)

// The gateway's own pages, which the app links to in the web version. They live
// here rather than in the gateway so that the services can hand them to the
// frontend without depending on the gateway.
const (
	AccountPath = "/-/account"
	AdminPath   = "/-/admin"
	LogoutPath  = "/-/logout"
)

// User is who a call is being made for.
type User struct {
	ID       string
	Username string
	Name     string
	Admin    bool
}

var (
	// ErrNotAdmin is what an administrative call made by anyone else answers.
	ErrNotAdmin = errors.New("only an administrator can change this")
	// ErrTaken is what claiming an ID someone else holds answers.
	ErrTaken = errors.New("that id is in use by someone else")
)

type userKey struct{}

type clientKey struct{}

// WithUser returns a context carrying u.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// FromContext returns the user a call is being made for, and false for a call
// with none -- which is every call in the desktop app.
func FromContext(ctx context.Context) (User, bool) {
	if ctx == nil {
		return User{}, false
	}
	u, ok := ctx.Value(userKey{}).(User)
	return u, ok
}

// WithClient returns a context naming the browser tab a call came from: the
// runtime's client ID, which the tab's event socket carries too. It is what
// lets a tab that has gone away have its streams closed behind it.
func WithClient(ctx context.Context, client string) context.Context {
	return context.WithValue(ctx, clientKey{}, client)
}

// Client returns the browser tab a call came from, empty when unknown.
func Client(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	c, _ := ctx.Value(clientKey{}).(string)
	return c
}

// RequireAdmin refuses a call made for a user who is not an administrator. A
// call made for nobody is the desktop app's, and passes.
func RequireAdmin(ctx context.Context) error {
	u, ok := FromContext(ctx)
	if ok && !u.Admin {
		return ErrNotAdmin
	}
	return nil
}
