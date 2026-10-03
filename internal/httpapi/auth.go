package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	foundationauth "tablescore-api/auth"
	"tablescore-api/internal/auth"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/store"
	"tablescore-api/services"
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
	if a.foundation != nil {
		token := strings.TrimPrefix(header, "Bearer ")
		if header == "" {
			if cookie, err := r.Cookie(foundationauth.CookieAccessToken); err == nil {
				token = cookie.Value
			}
		}
		if strings.Count(token, ".") == 2 {
			claims, err := foundationauth.ParseAccessToken(token, a.foundation.Cfg.Auth.JWTSecret)
			if err != nil {
				return domain.User{}, false, err
			}
			user, err := services.GetUserByID(a.foundation.DB, claims.UserID)
			if err != nil {
				return domain.User{}, false, err
			}
			return domain.User{ID: user.ID, Username: user.Username, CreatedAt: user.CreatedAt}, true, nil
		}
	}
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
	if a.foundation != nil && (strings.Count(token, ".") == 2 || token == "") {
		if cookie, err := r.Cookie(foundationauth.CookieRefreshToken); err == nil {
			_ = services.RevokeRefreshToken(a.foundation.DB, cookie.Value)
		}
		for _, item := range []struct{ name, path string }{{foundationauth.CookieAccessToken, "/"}, {foundationauth.CookieRefreshToken, foundationauth.RefreshTokenCookiePath}} {
			http.SetCookie(w, &http.Cookie{Name: item.name, Path: item.path, MaxAge: -1, HttpOnly: true, Secure: a.foundation.Cfg.IsProd(), SameSite: http.SameSiteLaxMode})
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
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

const maxAvatarBytes = 5 << 20

func (a *API) getMyAvatar(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	data, contentType, err := a.store.GetUserAvatar(user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *API) saveMyAvatar(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+(1<<20))
	if err := r.ParseMultipartForm(maxAvatarBytes); err != nil {
		writeError(w, http.StatusBadRequest, "an image up to 5 MB is required")
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxAvatarBytes {
		writeError(w, http.StatusBadRequest, "an image up to 5 MB is required")
		return
	}
	contentType := http.DetectContentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		writeError(w, http.StatusBadRequest, "JPEG, PNG or WebP image required")
		return
	}
	if err := a.store.SaveUserAvatar(user.ID, data, contentType); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) deleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteUserAvatar(user.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func tableWithToken(table domain.Table) map[string]any {
	return map[string]any{"code": table.Code, "name": table.Name, "hostToken": table.HostToken, "createdAt": table.CreatedAt}
}

func (a *API) getMyTables(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	tables, err := a.store.ListUserTables(user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	result := make([]map[string]any, 0, len(tables))
	for _, table := range tables {
		result = append(result, tableWithToken(table))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}

func (a *API) getMyRules(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	rules, err := a.store.ListUserRules(user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, rules)
}

func (a *API) claimTable(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var input struct{ Code, HostToken string }
	if !decodeJSON(w, r, &input) {
		return
	}
	table, err := a.store.GetTable(input.Code)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(table.HostToken), []byte(input.HostToken)) != 1 {
		writeError(w, http.StatusForbidden, "host token required")
		return
	}
	if err := a.store.LinkUserTable(user.ID, table.Code); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tableWithToken(table))
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
	if err := a.store.LinkUserTable(user.ID, table.Code); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := a.store.LinkUserSession(user.ID, input.SessionID, input.PlayerID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
