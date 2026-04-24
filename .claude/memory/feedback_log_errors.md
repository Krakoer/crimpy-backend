---
name: Always log the underlying error
description: When adding error logs, always include the actual Go error value
type: feedback
---

Always include the actual `err` (or equivalent) in log calls. A log like `slog.Error("failed to create training", "user_id", userID)` is useless without `"error", err`. Every error-path log must carry the underlying error.

**Why:** Without the actual error string, there is no way to debug what went wrong.

**How to apply:** Any time a handler or function logs a failure and returns, always add `"error", err` as a slog attribute. Also apply retroactively to existing logs that are missing it (e.g. existing `log.Printf("Warning: ...")` calls).
