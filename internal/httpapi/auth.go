package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"tablescore-api/internal/auth"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/store"
)

const authLifetime = 30 * 24 * time.Hour

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,30}$`)

type authResponse struct {
	User  domain.User `json:"user"`
	Token string      `json:"token"`
}

func (a *API) signUp(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct{ Username, Password string }
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if !usernamePattern.MatchString(input.Username) {
		writeError(w, http.StatusBadRequest, "username must be 3–30 letters, numbers, or underscores")
		return
	}
	passwordHash, err := auth.Hash(input.Password)
	if errors.Is(err, auth.ErrInvalidPassword) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}
	user, err := a.store.CreateUser(input.Username, passwordHash)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "username is already taken")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}
	a.startAuthSession(w, http.StatusCreated, user)
}

func (a *API) logIn(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct{ Username, Password string }
	if !decodeJSON(w, r, &input) {
		return
	}
	user, passwordHash, err := a.store.FindUser(strings.TrimSpace(input.Username))
	if err != nil || !auth.Verify(input.Password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	a.startAuthSession(w, http.StatusOK, user)
}

func (a *API) startAuthSession(w http.ResponseWriter, status int, user domain.User) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	if err := a.store.SaveAuthSession(user.ID, tokenHash(token), time.Now().Add(authLifetime).Unix()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, status, authResponse{User: user, Token: token})
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (a *API) optionalUser(r *http.Request) (domain.User, bool, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return domain.User{}, false, nil
	}
	if !strings.HasPrefix(header, "Bearer ") {
		return domain.User{}, false, store.ErrForbidden
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if len(token) != 43 {
		return domain.User{}, false, store.ErrForbidden
	}
	user, err := a.store.UserByAuthSession(tokenHash(token))
	return user, err == nil, err
}

func (a *API) requireUser(w http.ResponseWriter, r *http.Request) (domain.User, bool) {
	user, authenticated, err := a.optionalUser(r)
	if err != nil || !authenticated {
		writeError(w, http.StatusUnauthorized, "login required")
		return domain.User{}, false
	}
	return user, true
}

func (a *API) logOut(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireUser(w, r); !ok {
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err := a.store.DeleteAuthSession(tokenHash(token)); err != nil {
		writeError(w, http.StatusInternalServerError, "could not log out")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) getMe(w http.ResponseWriter, r *http.Request) {
	if user, ok := a.requireUser(w, r); ok {
		writeJSON(w, http.StatusOK, user)
	}
}

type mySessionResponse struct {
	sessionResponse
	GameName   string `json:"gameName"`
	MyPlayerID string `json:"myPlayerId"`
}

func (a *API) mySessions(userID string) ([]mySessionResponse, error) {
	linked, err := a.store.ListUserSessions(userID)
	if err != nil {
		return nil, err
	}
	result := make([]mySessionResponse, 0, len(linked))
	for _, item := range linked {
		session, err := a.sessionResult(item.Session)
		if err != nil {
			return nil, err
		}
		rule, err := a.store.GetRule(item.Session.RuleID)
		if err != nil {
			return nil, err
		}
		result = append(result, mySessionResponse{sessionResponse: session, GameName: rule.GameName, MyPlayerID: item.PlayerID})
	}
	return result, nil
}

func (a *API) getMySessions(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	sessions, err := a.mySessions(user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (a *API) getMyStats(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	sessions, err := a.mySessions(user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	stats := struct {
		FinishedGames int `json:"finishedGames"`
		Wins          int `json:"wins"`
		Ties          int `json:"ties"`
		TotalPoints   int `json:"totalPoints"`
	}{}
	for _, item := range sessions {
		if item.Status != "finished" {
			continue
		}
		stats.FinishedGames++
		for _, result := range item.Totals {
			if result.PlayerID == item.MyPlayerID {
				stats.TotalPoints += result.Total
			}
		}
		for _, winner := range item.Winners {
			if winner.PlayerID == item.MyPlayerID {
				if len(item.Winners) == 1 {
					stats.Wins++
				} else {
					stats.Ties++
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, stats)
}

func (a *API) claimSession(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var input struct{ SessionID, PlayerID, HostToken string }
	if !decodeJSON(w, r, &input) {
		return
	}
	session, err := a.store.GetSession(input.SessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	table, err := a.store.GetTable(session.TableCode)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(table.HostToken), []byte(input.HostToken)) != 1 {
		writeError(w, http.StatusForbidden, "host token required")
		return
	}
	if err := a.store.LinkUserSession(user.ID, input.SessionID, input.PlayerID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
