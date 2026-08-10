-- Adds SplitMixedFolder's synthetic-group bookkeeping to an
-- existing tagging_items table.  A fresh database never runs this
-- file: sql/schemas/tagging_items.sql already declares these
-- columns, so applySchema's isFreshDatabase check stamps this
-- version as applied without executing it.
ALTER TABLE tagging_items ADD COLUMN synthetic INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tagging_items ADD COLUMN parent_group_key TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_tagging_items_parent_group_key
    ON tagging_items(parent_group_key) WHERE parent_group_key != '';
