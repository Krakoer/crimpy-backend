-- Rename the "section" training item concept to "group": rename the
-- section_title column and migrate the type discriminator on existing rows.
ALTER TABLE "training_items" RENAME COLUMN "section_title" TO "group_title";
UPDATE "training_items" SET "type" = 'group' WHERE "type" = 'section';
