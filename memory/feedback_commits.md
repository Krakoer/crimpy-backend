---
name: Commit workflow preference
description: How the user wants git commits structured and formatted
type: feedback
---

No co-author lines in commit messages.

Small atomic commits with max 2-line messages (subject + blank + optional one-liner body).
Run `go fmt ./...` and `go build ./...` before every commit.

**Why:** User explicitly rejected a commit with a Co-Authored-By trailer and prefers clean, concise messages.

**How to apply:** Never add `Co-Authored-By` lines. Keep commit messages short. Always format and build before committing.
