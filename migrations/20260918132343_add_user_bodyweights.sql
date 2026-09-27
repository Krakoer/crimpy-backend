-- Create "user_bodyweights" table
CREATE TABLE "user_bodyweights" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "weight_kg" real NOT NULL,
  "measured_at" timestamptz NOT NULL DEFAULT now(),
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "user_bodyweights_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_bodyweights_weight_check" CHECK ((weight_kg > (0)::double precision) AND (weight_kg <= (500)::double precision))
);
-- Create index "user_bodyweights_user_id_measured_at_idx" to table: "user_bodyweights"
CREATE INDEX "user_bodyweights_user_id_measured_at_idx" ON "user_bodyweights" ("user_id", "measured_at" DESC);
