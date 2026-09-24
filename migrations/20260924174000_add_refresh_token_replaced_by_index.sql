-- Create index "refresh_tokens_replaced_by_idx" to table: "refresh_tokens"
CREATE INDEX "refresh_tokens_replaced_by_idx" ON "refresh_tokens" ("replaced_by");
