package store

import (
	"errors"
	"sort"
	"strings"
	"time"

	"tablescore-api/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *PostgresStore) SaveRulebooks(books []domain.Rulebook) error {
	if len(books) == 0 {
		return nil
	}
	return s.orm.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source"}, {Name: "source_id"}, {Name: "language"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "pdf_url", "updated_at"}),
	}).Create(&books).Error
}

func (s *PostgresStore) FindRulebooks(query, language string) ([]domain.Rulebook, error) {
	books := []domain.Rulebook{}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(query) + "%"
	err := s.orm.Where("language = ? AND name ILIKE ?", language, pattern).Order("name, id").Limit(50).Find(&books).Error
	return books, err
}

func (s *PostgresStore) GetRulebook(id string) (domain.Rulebook, error) {
	var book domain.Rulebook
	err := s.orm.First(&book, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return book, ErrNotFound
	}
	return book, err
}

func (s *MemoryStore) SaveRulebooks(books []domain.Rulebook) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, book := range books {
		if old, ok := s.rulebooks[book.ID]; ok {
			book.CreatedAt, book.Edition = old.CreatedAt, old.Edition
		}
		book.UpdatedAt = time.Now().UTC()
		s.rulebooks[book.ID] = book
	}
	return nil
}

func (s *MemoryStore) FindRulebooks(query, language string) ([]domain.Rulebook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	books := []domain.Rulebook{}
	for _, book := range s.rulebooks {
		if book.Language == language && strings.Contains(strings.ToLower(book.Name), strings.ToLower(query)) {
			books = append(books, book)
		}
	}
	sort.Slice(books, func(i, j int) bool { return books[i].Name < books[j].Name })
	if len(books) > 50 {
		books = books[:50]
	}
	return books, nil
}

func (s *MemoryStore) GetRulebook(id string) (domain.Rulebook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	book, ok := s.rulebooks[id]
	if !ok {
		return book, ErrNotFound
	}
	return book, nil
}
