package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rogerwesterbo/k8sdockside/internal/session"
)

const (
	roleAdmin = "admin"
	roleUser  = "user"
)

var (
	errLastAdmin     = errors.New("there must always be at least one administrator who can sign in")
	errUsernameTaken = errors.New("that username is taken")
	errAlreadySetUp  = errors.New("an administrator has already been created")
	errNoSuchUser    = errors.New("there is no such user")
	errNoSuchThing   = errors.New("there is no such provider")
	errProviderTaken = errors.New("a provider with that id already exists")
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)

// User is someone who may sign in.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name,omitempty"`
	// Email is kept only when it was typed by an administrator or vouched for
	// by a provider: it is what a provider sign-in is matched against.
	Email        string     `json:"email,omitempty"`
	Role         string     `json:"role"`
	PasswordHash string     `json:"passwordHash,omitempty"`
	Identities   []Identity `json:"identities,omitempty"`
	Disabled     bool       `json:"disabled,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastLoginAt  time.Time  `json:"lastLoginAt,omitzero"`
}

// Identity links a user to an account with a sign-in provider.
type Identity struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
	Email    string `json:"email,omitempty"`
}

// Admin reports whether the user is an administrator.
func (u User) Admin() bool { return u.Role == roleAdmin }

// DisplayName is the name to show for the user.
func (u User) DisplayName() string {
	if u.Name != "" {
		return u.Name
	}
	return u.Username
}

// HasPassword reports whether the user can sign in with a password.
func (u User) HasPassword() bool { return u.PasswordHash != "" }

// sessionUser is the user as the app's services see it.
func (u User) sessionUser() session.User {
	return session.User{ID: u.ID, Username: u.Username, Name: u.DisplayName(), Admin: u.Admin()}
}

// storedSession is one sign-in. Only a hash of its token is kept, so a copy of
// the data directory is not a pocketful of live sessions.
type storedSession struct {
	Hash      string    `json:"hash"`
	UserID    string    `json:"userId"`
	CSRF      string    `json:"csrf"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type state struct {
	Version   int             `json:"version"`
	Users     []User          `json:"users"`
	Sessions  []storedSession `json:"sessions"`
	Providers []Provider      `json:"providers"`
}

// clone copies the state deeply enough that changing the copy cannot change
// the original, so a change that fails to save can be thrown away.
func (st state) clone() state {
	out := state{
		Version:   st.Version,
		Users:     make([]User, len(st.Users)),
		Sessions:  slices.Clone(st.Sessions),
		Providers: make([]Provider, len(st.Providers)),
	}
	for i, u := range st.Users {
		u.Identities = slices.Clone(u.Identities)
		out.Users[i] = u
	}
	for i, p := range st.Providers {
		p.Scopes = slices.Clone(p.Scopes)
		p.AllowedDomains = slices.Clone(p.AllowedDomains)
		out.Providers[i] = p
	}
	return out
}

// hasAdmin reports whether somebody can administer the app.
func (st *state) hasAdmin() bool {
	return slices.ContainsFunc(st.Users, func(u User) bool { return u.Admin() && !u.Disabled })
}

func (st *state) userIndex(id string) int {
	return slices.IndexFunc(st.Users, func(u User) bool { return u.ID == id })
}

func (st *state) usernameTaken(name, except string) bool {
	return slices.ContainsFunc(st.Users, func(u User) bool {
		return u.ID != except && strings.EqualFold(u.Username, name)
	})
}

// store keeps users, sessions and the providers added through the admin page
// in one JSON file in the data directory. Every change is written through
// before it is answered, by writing a new file and renaming it over the old
// one, so a crash leaves one version or the other and never half of each.
type store struct {
	path string
	now  func() time.Time

	mu sync.Mutex
	st state
}

func openStore(path string) (*store, error) {
	s := &store{path: path, now: time.Now, st: state{Version: 1}}
	raw, err := os.ReadFile(path) // #nosec G304 -- the path is built from the configured data directory
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.st); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return s, nil
}

// mutate applies fn to a copy of the state and, if the result is sound and
// saves, keeps it.
//
// Sound means no change takes away the last administrator. A state that had
// none to begin with -- a file edited by hand -- is not refused every change
// for it, or nobody could so much as sign in to put it right.
func (s *store) mutate(fn func(st *state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.st.clone()
	if err := fn(&next); err != nil {
		return err
	}
	if s.st.hasAdmin() && !next.hasAdmin() {
		return errLastAdmin
	}
	if err := s.write(next); err != nil {
		return err
	}
	s.st = next
	return nil
}

func (s *store) write(st state) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".auth-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

// ---- users -----------------------------------------------------------------

func (s *store) userCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.Users)
}

func (s *store) users() []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.st.clone().Users
	slices.SortFunc(out, func(a, b User) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

func (s *store) find(match func(User) bool) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.st.Users {
		if match(u) {
			u.Identities = slices.Clone(u.Identities)
			return u, true
		}
	}
	return User{}, false
}

func (s *store) user(id string) (User, bool) {
	if id == "" {
		return User{}, false
	}
	return s.find(func(u User) bool { return u.ID == id })
}

func (s *store) userByUsername(name string) (User, bool) {
	if name == "" {
		return User{}, false
	}
	return s.find(func(u User) bool { return strings.EqualFold(u.Username, name) })
}

func (s *store) userByIdentity(provider, subject string) (User, bool) {
	if provider == "" || subject == "" {
		return User{}, false
	}
	return s.find(func(u User) bool {
		return slices.ContainsFunc(u.Identities, func(id Identity) bool {
			return id.Provider == provider && id.Subject == subject
		})
	})
}

// userByEmail finds the one user with an email address. Two users sharing one
// is ambiguous, and an ambiguous match is no match: signing in as whichever
// came first would be a guess.
func (s *store) userByEmail(email string) (User, bool) {
	if email == "" {
		return User{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []User
	for _, u := range s.st.Users {
		if strings.EqualFold(u.Email, email) {
			found = append(found, u)
		}
	}
	if len(found) != 1 {
		return User{}, false
	}
	found[0].Identities = slices.Clone(found[0].Identities)
	return found[0], true
}

// createUser adds a user. With first set it succeeds only while there are no
// users at all, which is what makes the setup page safe to race.
func (s *store) createUser(u User, first bool) (User, error) {
	u.Username = strings.TrimSpace(u.Username)
	u.Name = strings.TrimSpace(u.Name)
	if !usernamePattern.MatchString(u.Username) {
		return User{}, errors.New("a username is letters, digits, dots, dashes, underscores and @, starting with a letter or digit, at most 64 characters")
	}
	if u.Role != roleAdmin {
		u.Role = roleUser
	}
	u.ID = randomID()
	u.CreatedAt = s.now().UTC()

	err := s.mutate(func(st *state) error {
		if first && len(st.Users) > 0 {
			return errAlreadySetUp
		}
		if st.usernameTaken(u.Username, "") {
			return errUsernameTaken
		}
		st.Users = append(st.Users, u)
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// updateUser changes one user through fn.
func (s *store) updateUser(id string, fn func(u *User) error) (User, error) {
	var updated User
	err := s.mutate(func(st *state) error {
		i := st.userIndex(id)
		if i < 0 {
			return errNoSuchUser
		}
		if err := fn(&st.Users[i]); err != nil {
			return err
		}
		if st.usernameTaken(st.Users[i].Username, id) {
			return errUsernameTaken
		}
		if st.Users[i].Disabled {
			st.Sessions = slices.DeleteFunc(st.Sessions, func(ss storedSession) bool { return ss.UserID == id })
		}
		updated = st.Users[i]
		return nil
	})
	return updated, err
}

// deleteUser removes a user and every session of theirs.
func (s *store) deleteUser(id string) error {
	return s.mutate(func(st *state) error {
		i := st.userIndex(id)
		if i < 0 {
			return errNoSuchUser
		}
		st.Users = slices.Delete(st.Users, i, i+1)
		st.Sessions = slices.DeleteFunc(st.Sessions, func(ss storedSession) bool { return ss.UserID == id })
		return nil
	})
}

// freeUsername turns the first usable candidate into a username nobody has.
func (s *store) freeUsername(candidates ...string) string {
	base := ""
	for _, c := range candidates {
		if base = sanitizeUsername(c); base != "" {
			break
		}
	}
	if base == "" {
		base = "user"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name := base
	for n := 2; s.st.usernameTaken(name, ""); n++ {
		suffix := fmt.Sprintf("-%d", n)
		name = base[:min(len(base), 64-len(suffix))] + suffix
	}
	return name
}

// sanitizeUsername makes a username out of whatever a provider called someone.
func sanitizeUsername(raw string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '@', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.TrimLeft(b.String(), "._@-")
	if len(name) > 64 {
		name = name[:64]
	}
	if !usernamePattern.MatchString(name) {
		return ""
	}
	return name
}

// ---- sessions --------------------------------------------------------------

// newSession signs a user in: it makes a session, records the sign-in, and
// returns the token for the cookie. The token itself is not kept.
func (s *store) newSession(userID string, ttl time.Duration) (string, storedSession, error) {
	token := randomToken(32)
	now := s.now().UTC()
	sess := storedSession{
		Hash:      hashToken(token),
		UserID:    userID,
		CSRF:      randomToken(24),
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	err := s.mutate(func(st *state) error {
		i := st.userIndex(userID)
		if i < 0 {
			return errNoSuchUser
		}
		st.Users[i].LastLoginAt = now
		st.Sessions = slices.DeleteFunc(st.Sessions, func(ss storedSession) bool { return !ss.ExpiresAt.After(now) })
		st.Sessions = append(st.Sessions, sess)
		return nil
	})
	if err != nil {
		return "", storedSession{}, err
	}
	return token, sess, nil
}

// sessionFor resolves a cookie's token to its session and user. An expired
// session, or one whose user has gone or been switched off, is no session.
func (s *store) sessionFor(token string) (storedSession, User, bool) {
	if token == "" {
		return storedSession{}, User{}, false
	}
	hash := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, sess := range s.st.Sessions {
		if sess.Hash != hash {
			continue
		}
		if !sess.ExpiresAt.After(now) {
			return storedSession{}, User{}, false
		}
		i := s.st.userIndex(sess.UserID)
		if i < 0 || s.st.Users[i].Disabled {
			return storedSession{}, User{}, false
		}
		u := s.st.Users[i]
		u.Identities = slices.Clone(u.Identities)
		return sess, u, true
	}
	return storedSession{}, User{}, false
}

// sessionValid reports whether a session still stands: for the event socket,
// which outlives the request that opened it.
func (s *store) sessionValid(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, sess := range s.st.Sessions {
		if sess.Hash == hash {
			i := s.st.userIndex(sess.UserID)
			return sess.ExpiresAt.After(now) && i >= 0 && !s.st.Users[i].Disabled
		}
	}
	return false
}

// endSession signs one session out.
func (s *store) endSession(token string) error {
	hash := hashToken(token)
	return s.mutate(func(st *state) error {
		st.Sessions = slices.DeleteFunc(st.Sessions, func(ss storedSession) bool { return ss.Hash == hash })
		return nil
	})
}

// purge forgets expired sessions.
func (s *store) purge() error {
	now := s.now()
	s.mu.Lock()
	stale := slices.ContainsFunc(s.st.Sessions, func(ss storedSession) bool { return !ss.ExpiresAt.After(now) })
	s.mu.Unlock()
	if !stale {
		return nil
	}
	return s.mutate(func(st *state) error {
		st.Sessions = slices.DeleteFunc(st.Sessions, func(ss storedSession) bool { return !ss.ExpiresAt.After(now) })
		return nil
	})
}

// ---- providers -------------------------------------------------------------

func (s *store) providers() []Provider {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.clone().Providers
}

func (s *store) provider(id string) (Provider, bool) {
	for _, p := range s.providers() {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// putProvider adds a provider, or replaces the one called original.
func (s *store) putProvider(p Provider, original string) error {
	return s.mutate(func(st *state) error {
		taken := slices.IndexFunc(st.Providers, func(q Provider) bool { return q.ID == p.ID })
		if original == "" {
			if taken >= 0 {
				return errProviderTaken
			}
			st.Providers = append(st.Providers, p)
			return nil
		}
		i := slices.IndexFunc(st.Providers, func(q Provider) bool { return q.ID == original })
		if i < 0 {
			return errNoSuchThing
		}
		if taken >= 0 && taken != i {
			return errProviderTaken
		}
		st.Providers[i] = p
		return nil
	})
}

func (s *store) deleteProvider(id string) error {
	return s.mutate(func(st *state) error {
		i := slices.IndexFunc(st.Providers, func(q Provider) bool { return q.ID == id })
		if i < 0 {
			return errNoSuchThing
		}
		st.Providers = slices.Delete(st.Providers, i, i+1)
		return nil
	})
}

// ---- tokens ----------------------------------------------------------------

// randomToken returns n random bytes, base64url-encoded.
func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read does not fail; it crashes the program first
	return base64.RawURLEncoding.EncodeToString(b)
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
