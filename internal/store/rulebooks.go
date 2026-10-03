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
			book.CreatedAt, book.Edition, book.BGGID = old.CreatedAt, old.Edition, old.BGGID
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

func (s *PostgresStore) LinkRulebook(id string, bggID int) error {
	result := s.orm.Model(&domain.Rulebook{}).Where("id = ?", id).Update("bgg_id", bggID)
	if result.Error == nil && result.RowsAffected == 0 {
		return ErrNotFound
	}
	return result.Error
}

func (s *PostgresStore) GameRulebooks(bggID int) ([]domain.Rulebook, error) {
	books := []domain.Rulebook{}
	err := s.orm.Where("bgg_id = ?", bggID).Order("name, id").Find(&books).Error
	return books, err
}

func (s *PostgresStore) SaveRulebookPages(id string, pages []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM rulebook_pages WHERE rulebook_id = $1`, id); err != nil {
		return err
	}
	for i, text := range pages {
		if _, err := tx.Exec(`INSERT INTO rulebook_pages (rulebook_id, page, text) VALUES ($1, $2, $3)`, id, i+1, text); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) RulebookPages(id string) ([]string, error) {
	rows, err := s.db.Query(`SELECT text FROM rulebook_pages WHERE rulebook_id = $1 ORDER BY page`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	pages := []string{}
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, err
		}
		pages = append(pages, text)
	}
	return pages, rows.Err()
}

func (s *MemoryStore) LinkRulebook(id string, bggID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	book, ok := s.rulebooks[id]
	if !ok {
		return ErrNotFound
	}
	book.BGGID = &bggID
	s.rulebooks[id] = book
	return nil
}

func (s *MemoryStore) GameRulebooks(bggID int) ([]domain.Rulebook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	books := []domain.Rulebook{}
	for _, book := range s.rulebooks {
		if book.BGGID != nil && *book.BGGID == bggID {
			books = append(books, book)
		}
	}
	sort.Slice(books, func(i, j int) bool { return books[i].Name < books[j].Name })
	return books, nil
}

func (s *MemoryStore) SaveRulebookPages(id string, pages []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rulebooks[id]; !ok {
		return ErrNotFound
	}
	s.bookPages[id] = append([]string(nil), pages...)
	return nil
}

func (s *MemoryStore) RulebookPages(id string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string{}, s.bookPages[id]...), nil
}
