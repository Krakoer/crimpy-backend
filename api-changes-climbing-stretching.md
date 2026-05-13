# API Changes: Training Types + Stretching System Tag

These changes add training type differentiation and a system-level immutable Stretching tag for exercises.

There are three training types:

| `training_type` | Description |
|---|---|
| `workout` | Structured training with sections, circuits, exercises, and hangboards (the existing item tree) |
| `stretching` | Stretching session - same item tree, but the frontend presents a simplified creation UI |
| `climbing` | On-the-wall training (pyramid, volume, etc.) - free-form, described via `title`/`description`/`goal`/`comment` only, **no items** |

---

## 1. Tags

### New field on all tag responses

Every endpoint that returns a tag now includes `is_builtin: bool`.

```json
{
  "id": "...",
  "name": "Stretching",
  "color": "#4DB6AC",
  "is_builtin": true,
  "created_at": "...",
  "updated_at": "..."
}
```

### System Stretching tag

`GET /api/coach/tags` now always returns the system Stretching tag (seeded in the database) alongside the coach's own tags. It appears first in the list (sorted builtin-first, then alphabetical).

- `is_builtin: true` means the tag is immutable.
- Do not show edit/delete UI for tags where `is_builtin === true`.

### Attempting to modify a builtin tag returns 403

```
PUT  /api/coach/tags/{id}    -> 403 { "error": "Cannot modify a system tag" }
DELETE /api/coach/tags/{id}  -> 403 { "error": "Cannot modify a system tag" }
```

### Assigning/unassigning builtin tags to exercises

Coaches can assign and unassign builtin tags (including the Stretching tag) to their exercises using the same endpoints as regular tags:

```
POST   /api/coach/exercises/{exercise_id}/tags/{tag_id}   -> 200
DELETE /api/coach/exercises/{exercise_id}/tags/{tag_id}   -> 200
```

---

## 2. Coach Trainings

### New fields on create and update requests

Both `POST /api/coach/trainings` and `PUT /api/coach/trainings/{id}` now accept three new fields:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `training_type` | string | no | `"workout"` | `"workout"`, `"stretching"`, or `"climbing"` |
| `goal` | string | no | `""` | Coach's intent for this training |
| `comment` | string | no | `""` | Constraints or notes (e.g. "max RPE 8", "hold 30s each") |

Example create body:
```json
{
  "title": "Hip Mobility",
  "description": "Full stretching routine",
  "training_type": "stretching",
  "goal": "Improve hip flexibility",
  "comment": "Hold each position for 30 seconds",
  "items": [...]
}
```

### New fields on all training responses

Both `GET /api/coach/trainings` (list) and `GET /api/coach/trainings/{id}` (detail) now return these fields:

```json
{
  "id": "...",
  "coach_id": "...",
  "title": "Hip Mobility",
  "description": "...",
  "training_type": "stretching",
  "goal": "Improve hip flexibility",
  "comment": "Hold each position for 30 seconds",
  "items": [...],
  "created_at": "...",
  "updated_at": "..."
}
```

Existing trainings without the new fields will have `training_type: "workout"`, `goal: ""`, `comment: ""`.

---

## Integration Notes

- **Workout creation UI**: Omit `training_type` or send `"workout"`. Full hangboard/circuit/section item structure applies. This was previously the only type.
- **Stretching creation UI**: Send `training_type: "stretching"`. Same item tree as workout, but the frontend presents a simplified interface (e.g. single circuit of stretching exercises). The backend does not enforce structural constraints.
- **Climbing creation UI**: Send `training_type: "climbing"`. Only the scalar fields are used (`title`, `description`, `goal`, `comment`). Send `items: []` or omit `items` entirely - the backend stores and returns an empty array.
- **Tag list rendering**: Filter on `is_builtin` to conditionally hide edit/delete actions. The Stretching tag has a fixed color `#4DB6AC`.
- **No breaking changes to existing item structure**: `coach_training_items` schema is unchanged.
