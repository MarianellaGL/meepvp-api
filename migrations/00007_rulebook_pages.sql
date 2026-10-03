-- +goose Up
-- A game is identified by its BGG ID; a rulebook links to it once matched.
ALTER TABLE rulebooks ADD COLUMN bgg_id INTEGER;
CREATE INDEX rulebooks_bgg_id_idx ON rulebooks(bgg_id) WHERE bgg_id IS NOT NULL;
-- Extracted text is stored once per page so rulebooks are read only once.
CREATE TABLE rulebook_pages (
    rulebook_id TEXT NOT NULL REFERENCES rulebooks(id) ON DELETE CASCADE,
    page INTEGER NOT NULL CHECK (page > 0),
    text TEXT NOT NULL,
    PRIMARY KEY (rulebook_id, page)
);

-- +goose Down
DROP TABLE rulebook_pages;
DROP INDEX rulebooks_bgg_id_idx;
ALTER TABLE rulebooks DROP COLUMN bgg_id;
