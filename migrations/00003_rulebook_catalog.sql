-- +goose Up
CREATE TABLE rulebooks (
    id text PRIMARY KEY,
    source text NOT NULL,
    source_id text NOT NULL,
    name text NOT NULL,
    language text NOT NULL,
    edition text NOT NULL DEFAULT '',
    pdf_url text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, source_id, language)
);
CREATE INDEX rulebooks_language_idx ON rulebooks (language);

INSERT INTO rulebooks (id, source, source_id, name, language, edition, pdf_url)
VALUES
('rule-book:en:everdell', 'rule-book.org', 'everdell', 'Everdell Rulebook', 'en', 'Base game', 'https://cdn.1j1ju.com/medias/c6/cd/89-everdell-rulebook.pdf'),
('rule-book:en:catan', 'rule-book.org', 'catan', 'Catan Rulebook', 'en', 'Base game', 'https://cdn.1j1ju.com/medias/7a/18/fd-catan-rulebook.pdf');

-- +goose Down
DROP TABLE rulebooks;
