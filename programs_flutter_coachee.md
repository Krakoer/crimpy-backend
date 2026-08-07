# Programs API -- Flutter Coachee Integration Guide

This document is addressed to the Flutter app implementing the **coachee (athlete) side**
of the Programs feature. It covers only the user-facing read-only endpoints, the response
shapes, and how to render per-item overrides on top of training templates.

All requests require `Authorization: Bearer <jwt>`.
Base URL: `https://api.portfolio-online.ovh` (prod) / `http://localhost:3000` (dev).

---

## Concept overview

```
Program
  |-- metadata only (name, objective, start_date, duration_weeks)
  |
  +-- Week 1  (explicitly defined by the coach)
  |     +-- Session A  (references a coach_training by ID)
  |     |     +-- Override for item X  (sparse JSON -- only changed fields)
  |     +-- Session B
  |
  +-- Week 2  (different schedule than week 1)
  |     ...
  +-- Week 3  (undefined -- no entry, coach has not set it yet)
```

A **program** contains no schedule by itself. The schedule is in **weeks**.
Only weeks the coach has explicitly defined are returned -- a 6-week program
may have only weeks 1, 2, and 5 defined. Weeks 3, 4, 6 would return 404.

A **session** within a week is a reference to an existing coach training template,
plus optional **overrides** -- a sparse JSON object containing only the fields
that differ from the template for that week (e.g. heavier load, fewer reps).

---

## Endpoints

### List my programs

```
GET /api/user/programs
```

Response 200:
```json
[
  {
    "id": "uuid",
    "coach_id": "uuid",
    "user_id": "uuid",
    "name": "6-Week Finger Strength Block",
    "objective": "Build max finger strength on crimps",
    "start_date": "2026-06-01",
    "duration_weeks": 6,
    "created_at": "2026-06-01T10:00:00Z",
    "updated_at": "2026-06-01T10:00:00Z"
  }
]
```

`objective` and `duration_weeks` are omitted when null (not set by the coach).

---

### Get one program

```
GET /api/user/programs/:program_id
```

Response 200: same shape as a single item from the list above.
Response 403 if the program belongs to a different user.
Response 404 if the program does not exist.

---

### List weeks of a program (summary)

```
GET /api/user/programs/:program_id/weeks
```

Response 200:
```json
[
  {
    "id": "uuid",
    "program_id": "uuid",
    "week_number": 1,
    "notes": "First heavy week",
    "created_at": "...",
    "updated_at": "..."
  },
  {
    "id": "uuid",
    "program_id": "uuid",
    "week_number": 2,
    "created_at": "...",
    "updated_at": "..."
  }
]
```

`notes` is omitted when null. The array is ordered by `week_number`.
Only weeks the coach has defined are included.

---

### Get a week (full detail)

```
GET /api/user/programs/:program_id/weeks/:week_number
```

`week_number` is an integer starting at 1.

Response 200:
```json
{
  "id": "uuid",
  "program_id": "uuid",
  "week_number": 1,
  "notes": "First heavy week",
  "sessions": [
    {
      "id": "uuid",
      "training_id": "uuid",
      "training_title": "Max Hangs",
      "training_type": "climbing",
      "day_of_week": 0,
      "position": 0,
      "notes": "Focus on max load",
      "overrides": [
        {
          "id": "uuid",
          "item_id": "uuid",
          "overrides": {
            "loads": [{ "value": 35, "unit": "kg" }],
            "reps": 6
          }
        }
      ]
    },
    {
      "id": "uuid",
      "training_id": "uuid",
      "training_title": "Mobility",
      "training_type": "workout",
      "times_per_week": 2,
      "position": 1,
      "overrides": []
    }
  ],
  "created_at": "...",
  "updated_at": "..."
}
```

Field notes:
- `day_of_week`: 0=Monday ... 6=Sunday; mutually exclusive with `times_per_week`
- `times_per_week`: how many times per week (unscheduled); mutually exclusive with `day_of_week`
- One of the two is always set, never both, never neither.
- `position`: 0-based display order within the week
- `training_title` and `training_type` are denormalized -- no extra fetch needed
- `overrides` is always present as an array (empty `[]` when none)
- `notes` on week and session are omitted when null

Response 404 if the week has not been defined by the coach yet.
Response 403 if the program belongs to a different user.

---

## Rendering overrides

Each session references a `training_id`. To show the actual exercise content,
fetch the coach training separately:

```
GET /api/coach/trainings/:training_id   -- if the endpoint is exposed to users
```

(Check with the backend team whether a user-facing GET for coach trainings is available,
or if you receive the full training tree separately.)

Once you have the training tree, for each `coach_training_item` in the tree:

1. Look up whether `overrides` contains an entry with `item_id` matching that item.
2. If found, merge the override object on top of the item's base values.
3. Only the keys present in the override JSON should replace the base values.
   Missing keys retain the template value.

### Override keys and their meaning

These map directly to fields on a `coach_training_item`:

| Override key          | Type                              | Applies to                  |
|-----------------------|-----------------------------------|-----------------------------|
| `loads`               | `[{value: float, unit: string}]`  | exercise, hangboard (right) |
| `left_loads`          | `[{value: float, unit: string}]`  | hangboard split mode (left) |
| `hand_positions`      | `[[string]]`, one array per hand  | exercise / hangboard        |
| `edge_sizes_mm`       | `[int]`                           | hangboard                   |
| `reps`                | `int`                             | exercise, hangboard         |
| `cycles`              | `int`                             | circuit, hangboard          |
| `cycle_rest_seconds`  | `int`                             | circuit, hangboard          |
| `rest_seconds`        | `int`                             | exercise, hangboard         |
| `hb_worktime_seconds` | `int`                             | hangboard                   |
| `both_hands`          | `bool`                            | hangboard                   |

Array fields (`loads`, `left_loads`, `hand_positions`, `edge_sizes_mm`) carry
one entry per configuration slot. Their length tells the granularity apart:

| Length          | Meaning                                                   |
|-----------------|-----------------------------------------------------------|
| `1`             | uniform, the same value for every rep of every set        |
| `reps`          | one value per rep, repeated in every set                  |
| `cycles * reps` | one value per set and rep, indexed `set * reps + rep`     |

Split-hand loads keep their existing interleaving, so `loads` holds twice as
many entries: left at `2 * i` and right at `2 * i + 1`.

A client that only understands the per-rep layout can read the first `reps`
entries and get the configuration of the first set.

### Merge example (Dart pseudocode)

```dart
TrainingItem effectiveItem(TrainingItem base, Map<String, dynamic>? overrides) {
  if (overrides == null || overrides.isEmpty) return base;
  return base.copyWith(
    loads:              overrides['loads']              ?? base.loads,
    leftLoads:          overrides['left_loads']         ?? base.leftLoads,
    handPositions:      overrides['hand_positions']     ?? base.handPositions,
    edgeSizesMm:        overrides['edge_sizes_mm']      ?? base.edgeSizesMm,
    reps:               overrides['reps']               ?? base.reps,
    cycles:             overrides['cycles']             ?? base.cycles,
    cycleRestSeconds:   overrides['cycle_rest_seconds'] ?? base.cycleRestSeconds,
    restSeconds:        overrides['rest_seconds']       ?? base.restSeconds,
    hbWorktimeSeconds:  overrides['hb_worktime_seconds']?? base.hbWorktimeSeconds,
    bothHands:          overrides['both_hands']         ?? base.bothHands,
  );
}
```

---

## Suggested Dart models

```dart
class Program {
  final String id;
  final String coachId;
  final String userId;
  final String name;
  final String? objective;
  final DateTime startDate;
  final int? durationWeeks;
  final DateTime createdAt;
  final DateTime updatedAt;
}

class WeekSummary {
  final String id;
  final String programId;
  final int weekNumber;
  final String? notes;
  final DateTime createdAt;
  final DateTime updatedAt;
}

class Week {
  final String id;
  final String programId;
  final int weekNumber;
  final String? notes;
  final List<WeekSession> sessions;
  final DateTime createdAt;
  final DateTime updatedAt;
}

class WeekSession {
  final String id;
  final String trainingId;
  final String trainingTitle;
  final String trainingType;  // "workout" | "climbing" | "stretching" ...
  final int? dayOfWeek;       // 0=Mon ... 6=Sun; null if times_per_week is set
  final int? timesPerWeek;    // null if day_of_week is set
  final int position;
  final String? notes;
  final List<SessionOverride> overrides;
}

class SessionOverride {
  final String id;
  final String itemId;
  final Map<String, dynamic> overrides;  // sparse -- parse keys as needed
}
```

---

## Error codes

| Code | Meaning                                      |
|------|----------------------------------------------|
| 401  | Missing or invalid JWT                       |
| 403  | Program belongs to a different user          |
| 404  | Program or week not found                    |
| 500  | Server error                                 |

---

## Suggested fetch flow

```
On program list screen:
  GET /api/user/programs
  -> show list

On program detail screen:
  GET /api/user/programs/:id/weeks
  -> show week list (summary -- week number, notes, no sessions)

On week detail screen:
  GET /api/user/programs/:id/weeks/:n
  -> show sessions with training_title + training_type
  -> for each session, if overrides non-empty, fetch training items
     and apply override merge before rendering
```

Keep week detail lazy -- only fetch when the user taps into a specific week.
