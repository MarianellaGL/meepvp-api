package store

import (
	"strings"
	"time"

	"tablescore-api/internal/domain"
)

func (s *MemoryStore) CreateUser(username, passwordHash string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(username)
	if _, exists := s.users[key]; exists {
		return domain.User{}, ErrConflict
	}
	user := domain.User{ID: randomID(), Username: username, CreatedAt: time.Now().UTC()}
	s.users[key], s.passwords[user.ID] = user, passwordHash
	return user, nil
}

func (s *MemoryStore) FindUser(username string) (domain.User, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[strings.ToLower(username)]
	if !ok {
		return domain.User{}, "", ErrNotFound
	}
	return user, s.passwords[user.ID], nil
}

func (s *MemoryStore) SaveAuthSession(userID, tokenHash string, expiresAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authSessions[tokenHash] = memoryAuthSession{userID: userID, expiresAt: expiresAt}
	return nil
}

func (s *MemoryStore) UserByAuthSession(tokenHash string) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.authSessions[tokenHash]
	if !ok || session.expiresAt <= time.Now().Unix() {
		return domain.User{}, ErrNotFound
	}
	for _, user := range s.users {
		if user.ID == session.userID {
			return user, nil
		}
	}
	return domain.User{}, ErrNotFound
}

func (s *MemoryStore) DeleteAuthSession(tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.authSessions, tokenHash)
	return nil
}

func (s *MemoryStore) LinkUserSession(userID, sessionID, playerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	playerFound := false
	for _, player := range session.Players {
		if player.ID == playerID {
			playerFound = true
			break
		}
	}
	if !playerFound {
		return ErrValidation
	}
	for ownerID, memberships := range s.userSessions {
		if memberships[sessionID] == playerID {
			if ownerID == userID {
				return nil
			}
			return ErrConflict
		}
	}
	if s.userSessions[userID] == nil {
		s.userSessions[userID] = map[string]string{}
	}
	s.userSessions[userID][sessionID] = playerID
	return nil
}

func (s *MemoryStore) ListUserSessions(userID string) ([]domain.UserSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []domain.UserSession{}
	for sessionID, playerID := range s.userSessions[userID] {
		result = append(result, domain.UserSession{Session: s.sessions[sessionID], PlayerID: playerID})
	}
	return result, nil
}
