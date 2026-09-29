-- +goose Up
INSERT INTO rulebooks (id, source, source_id, name, language, edition, pdf_url)
VALUES ('rule-book:en:wingspan', 'rule-book.org', 'wingspan', 'Wingspan Rulebook', 'en', 'Base game', 'https://cdn.1j1ju.com/medias/ff/16/4c-wingspan-rulebook.pdf')
ON CONFLICT (source, source_id, language) DO NOTHING;

-- +goose Down
DELETE FROM rulebooks WHERE id = 'rule-book:en:wingspan';
