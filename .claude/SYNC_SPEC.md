# Cloud Sync Implementation Specification

> **Purpose:** Feed this document to Claude Code to implement cloud synchronisation across the backend and mobile app projects.  
> **Scope:** Local-first mobile app gaining optional cloud sync. Users can use the app without an account and opt in to sync later.  
> **Read both sections.** The Backend section and the Mobile App section are independent but must be implemented consistently.

---

## 0. Shared Conventions (apply to both projects)

### 0.1 Record Identity
- Every syncable record must have a **UUID v4** primary key generated on the client at creation time. Never use auto-increment integers for synced records.
- UUID generation must be done with a cryptographically safe library (e.g. `uuid` on JS/TS, `java.util.UUID` on Android, `Foundation.UUID` on iOS).

### 0.2 Timestamps
- All timestamps are **UTC ISO 8601** strings: `"2024-06-15T14:30:00.000Z"`.
- Every record carries: `created_at`, `updated_at`, `deleted_at` (nullable).
- The backend also stamps a `server_updated_at` on every write. This is the authoritative field for conflict resolution to avoid client clock skew.

### 0.3 Soft Deletes
- **Never hard-delete a synced record.** Set `deleted_at` to the current UTC timestamp instead.
- Hard deletes may be run server-side only after a tombstone retention period (suggested: 90 days).

### 0.4 Sync Versioning
- Each record carries a `sync_version` integer, starting at `1` and incremented by the server on every accepted write.
- The client stores `last_sync_version` in its local `sync_meta` table. Incremental sync requests use this as a cursor.

### 0.5 Device Identity
- Each app installation generates a persistent `device_id` (UUID v4) stored in secure local storage. Include it in every sync request header: `X-Device-Id`.

---

## 1. Backend

### 1.1 Database Schema Changes

Add the following columns to **every table that participates in sync**. Adjust for your ORM/migration tool:

```sql
-- Run for each synced table (replace `items` with the actual table name)
ALTER TABLE items
  ADD COLUMN IF NOT EXISTS id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  ADD COLUMN IF NOT EXISTS created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS deleted_at  TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS device_id   UUID,
  ADD COLUMN IF NOT EXISTS sync_version BIGINT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS server_updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE;

-- Index required for incremental pull queries
CREATE INDEX IF NOT EXISTS idx_items_account_sync
  ON items (account_id, sync_version)
  WHERE deleted_at IS NULL OR deleted_at IS NOT NULL; -- include tombstones
```

Create the `accounts` table if it does not already exist:

```sql
CREATE TABLE IF NOT EXISTS accounts (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email           TEXT UNIQUE NOT NULL,
  password_hash   TEXT NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at    TIMESTAMPTZ
);
```

### 1.2 Server-Side Trigger (auto-increment sync_version)

```sql
-- Trigger to auto-update server_updated_at and sync_version on every row write
CREATE OR REPLACE FUNCTION bump_sync_version()
RETURNS TRIGGER AS $$
BEGIN
  NEW.server_updated_at := now();
  NEW.sync_version := (
    SELECT COALESCE(MAX(sync_version), 0) + 1
    FROM items  -- replace with TG_TABLE_NAME if using a generic trigger
    WHERE account_id = NEW.account_id
  );
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_bump_sync_version
BEFORE INSERT OR UPDATE ON items
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();
```

> **Note for the implementer:** Create one trigger per synced table, or use a single generic PL/pgSQL function with `TG_TABLE_NAME` and dynamic SQL.

### 1.3 API Endpoints

All endpoints require `Authorization: Bearer <token>` and `X-Device-Id: <uuid>`.

---

#### `GET /sync/summary`
Returns a lightweight count of records per synced collection. Used by the mobile app at login time to detect whether the cloud account already has data.

**Response:**
```json
{
  "account_id": "uuid",
  "collections": {
    "items": 42,
    "categories": 5
  },
  "last_sync_version": 187
}
```

---

#### `GET /sync/pull?since_version=<n>&collections=items,categories`
Returns all records (including tombstones) modified after `since_version` for the given collections.

- If `since_version=0`, returns the full dataset (initial pull).
- Include records where `deleted_at IS NOT NULL` so the client can process tombstones.

**Response:**
```json
{
  "server_version": 220,
  "records": {
    "items": [
      {
        "id": "uuid",
        "title": "Example",
        "created_at": "2024-01-01T00:00:00.000Z",
        "updated_at": "2024-06-01T12:00:00.000Z",
        "deleted_at": null,
        "device_id": "uuid",
        "sync_version": 190,
        "server_updated_at": "2024-06-01T12:00:01.000Z"
      }
    ],
    "categories": []
  }
}
```

---

#### `POST /sync/push`
Accepts a batch of locally changed records from the client. The server applies last-write-wins (LWW) using `server_updated_at` as the authoritative timestamp.

**Request body:**
```json
{
  "records": {
    "items": [
      {
        "id": "uuid",
        "title": "My item",
        "created_at": "2024-06-01T10:00:00.000Z",
        "updated_at": "2024-06-01T10:00:00.000Z",
        "deleted_at": null,
        "device_id": "uuid",
        "sync_version": 0
      }
    ]
  }
}
```

**Server logic (implement in the handler):**
```
for each incoming record:
  existing = SELECT * FROM table WHERE id = record.id AND account_id = current_user.id

  if existing is null:
    INSERT record (new record from this device)

  elif record.updated_at > existing.server_updated_at:
    UPDATE existing with incoming data  -- incoming is newer: LWW

  else:
    SKIP  -- server version is newer; client will get it on next pull
```

**Response:**
```json
{
  "server_version": 225,
  "accepted": ["uuid1", "uuid2"],
  "rejected": [],
  "conflicts": []
}
```

> Conflicts array is reserved for future field-level merge support. For now it will always be empty.

---

#### `POST /sync/migrate`
Called once at first login when the client has local data and the cloud account is **empty** (`summary.collections` all zero). Bulk-inserts the full local dataset server-side.

**Request body:** same shape as `/sync/push`.  
**Response:** same shape as `/sync/push`.

This is a separate endpoint from `/sync/push` for clarity and rate-limiting purposes.

---

### 1.4 Auth Endpoints

These are standard; implement them if not already present.

- `POST /auth/register` — creates account, returns JWT.
- `POST /auth/login` — returns JWT.
- `POST /auth/logout` — invalidates token server-side (maintain a denylist or use short-lived JWTs with refresh tokens).

---

### 1.5 Security Requirements

- Enforce `account_id = current_user.id` in every query. Never trust the `account_id` sent in the request body.
- Rate-limit `/sync/push` and `/sync/migrate` to prevent abuse.
- Validate UUIDs on all `id` fields (reject malformed input with 400).

---

## 2. Mobile App

### 2.1 Local Database Schema Changes

Add the following columns to every local table that participates in sync. Adjust for your local DB library (SQLite, Room, CoreData, etc.):

```sql
-- Run migration for each local synced table
ALTER TABLE items ADD COLUMN sync_id       TEXT NOT NULL DEFAULT (lower(hex(randomblob(16))));
ALTER TABLE items ADD COLUMN created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'));
ALTER TABLE items ADD COLUMN updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'));
ALTER TABLE items ADD COLUMN deleted_at    TEXT;
ALTER TABLE items ADD COLUMN device_id     TEXT;
ALTER TABLE items ADD COLUMN sync_version  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE items ADD COLUMN is_dirty      INTEGER NOT NULL DEFAULT 1; -- 1 = needs push
```

Create a `sync_meta` table to track sync state:

```sql
CREATE TABLE IF NOT EXISTS sync_meta (
  key   TEXT PRIMARY KEY,
  value TEXT
);
-- Pre-insert default values
INSERT OR IGNORE INTO sync_meta VALUES ('last_sync_version', '0');
INSERT OR IGNORE INTO sync_meta VALUES ('account_id', NULL);
INSERT OR IGNORE INTO sync_meta VALUES ('last_synced_at', NULL);
INSERT OR IGNORE INTO sync_meta VALUES ('device_id', NULL);  -- set on first launch
```

### 2.2 Local Write Hook

Every local INSERT and UPDATE must:
1. Set `updated_at` to the current UTC timestamp.
2. Set `device_id` to the local `device_id` from `sync_meta`.
3. Set `is_dirty = 1`.
4. If `sync_id` is null, generate a UUID v4 and assign it.

Implement this as a database trigger or a repository-layer wrapper — **not** scattered across the UI layer.

```sql
-- Example SQLite trigger
CREATE TRIGGER trg_items_updated
AFTER UPDATE ON items
FOR EACH ROW
BEGIN
  UPDATE items SET
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'),
    is_dirty = 1
  WHERE rowid = NEW.rowid;
END;
```

### 2.3 Sync State Machine

The app has four sync states. Store this in a `SyncManager` singleton (or equivalent):

```
IDLE         → no sync in progress
SYNCING      → sync API calls in flight
PENDING      → dirty records exist but no active sync
ERROR        → last sync failed; will retry
```

Transitions:
- App foreground → `IDLE` becomes `SYNCING` (pull then push)
- Local write → `IDLE`/`ERROR` becomes `PENDING`; debounce 2 seconds then start push
- Success → return to `IDLE`, update `last_sync_version`
- Network error → `ERROR`; retry with exponential backoff (2s, 4s, 8s… max 5 min)
- User not logged in → stay `IDLE`; no sync runs

### 2.4 Login / Account Creation Flow

This is the most critical section. Implement exactly as described.

```
Step 1: User taps "Log in" or "Create account"
Step 2: Perform auth (POST /auth/login or /auth/register) → receive JWT
Step 3: GET /sync/summary → receive cloud record counts
Step 4: COUNT local records (excluding soft-deleted)
Step 5: Run the decision tree below
Step 6: Update sync_meta: set account_id, last_sync_version, last_synced_at
Step 7: Start ongoing sync
```

**Decision tree (Step 5):**

```
local_count = total local records (non-deleted)
cloud_count = sum of all values in summary.collections

if local_count == 0 AND cloud_count == 0:
  → Nothing to do. Cloud is empty, local is empty.

if local_count == 0 AND cloud_count > 0:
  → Pull only: GET /sync/pull?since_version=0
  → Write all received records into local DB
  → Set last_sync_version = server_version from response

if local_count > 0 AND cloud_count == 0:
  → Migrate: POST /sync/migrate with all local records
  → Set is_dirty = 0 for all migrated records
  → Set last_sync_version = server_version from response

if local_count > 0 AND cloud_count > 0:
  → COLLISION: show MergeDialog to user (see §2.5)
```

### 2.5 Merge Dialog (Collision Case)

Show a modal dialog. Do not proceed without explicit user input. Pre-select "Merge both."

```
┌──────────────────────────────────────────────────┐
│  You have data on this device and in the cloud.  │
│  How would you like to continue?                 │
│                                                  │
│  ● Merge both (recommended)                      │
│    Combine everything. If the same item was      │
│    changed in both places, the newest wins.      │
│                                                  │
│  ○ Use device data only                          │
│    Replace cloud data with what's on this        │
│    device. Your cloud data will be overwritten.  │
│                                                  │
│  ○ Use cloud data only                           │
│    Replace device data with your cloud data.     │
│    Local-only data will be lost.                 │
│                                                  │
│                           [Cancel]  [Continue →] │
└──────────────────────────────────────────────────┘
```

**Implement each choice:**

**"Merge both":**
```
1. GET /sync/pull?since_version=0  (fetch entire cloud dataset)
2. Run local merge algorithm (see §2.6)
3. POST /sync/push with all records that are still is_dirty=1 after merge
4. Set last_sync_version = server_version
```

**"Use device data only":**
```
1. POST /sync/migrate with all local records
   (server will overwrite existing cloud records with same IDs,
    and orphaned cloud records not present locally will remain —
    warn the user this could leave stale data if they've used
    another device; or implement a DELETE /sync/reset endpoint
    that clears all server-side records for the account first)
2. Set last_sync_version = server_version
```

**"Use cloud data only":**
```
1. DELETE all local records (hard delete is fine here — user chose this)
2. GET /sync/pull?since_version=0
3. Write all cloud records to local DB
4. Set last_sync_version = server_version
```

### 2.6 Local Merge Algorithm

```
function mergeRecords(localRecords, cloudRecords):
  allIds = union of local IDs and cloud IDs

  for each id in allIds:
    local  = localRecords.findById(id)   // may be null
    remote = cloudRecords.findById(id)   // may be null

    if local == null:
      // New record from cloud — insert locally
      INSERT remote into local DB
      SET is_dirty = 0

    elif remote == null:
      // Local-only record — will be pushed in next push step
      SET is_dirty = 1

    else:
      // Both exist — Last Write Wins on updated_at
      // Use server_updated_at from remote if available (avoids clock skew)
      remoteTimestamp = remote.server_updated_at ?? remote.updated_at
      localTimestamp  = local.updated_at

      if remoteTimestamp > localTimestamp:
        // Cloud version is newer — overwrite local
        UPDATE local DB with remote data
        SET is_dirty = 0
      elif localTimestamp > remoteTimestamp:
        // Local version is newer — keep local, mark for push
        SET is_dirty = 1
      else:
        // Exact tie — prefer local (no-op)
        SET is_dirty = 0

      // Always propagate soft deletes
      if remote.deleted_at != null AND local.deleted_at == null:
        if remoteTimestamp >= localTimestamp:
          SET local.deleted_at = remote.deleted_at
          SET is_dirty = 0
```

### 2.7 Ongoing Sync

After the initial login flow, sync runs continuously.

**Push (local → cloud):**
```
Trigger: on any local write, debounce 2 seconds
Action:
  1. SELECT * FROM items WHERE is_dirty = 1 (and all other synced tables)
  2. If empty → skip
  3. POST /sync/push with dirty records
  4. On success: SET is_dirty = 0 for accepted IDs
  5. Update last_synced_at in sync_meta
```

**Pull (cloud → local):**
```
Trigger: app comes to foreground, or every 5 minutes while foreground
Action:
  1. GET /sync/pull?since_version=<last_sync_version>
  2. Run merge algorithm for received records
  3. Update last_sync_version = server_version from response
```

**Ordering:** Always pull before push to minimise conflicts.

### 2.8 Logout

```
1. Call POST /auth/logout (optional — invalidate server-side if applicable)
2. Clear JWT from secure storage
3. Update sync_meta: set account_id = null, last_sync_version = 0, last_synced_at = null
4. Do NOT delete local data — user keeps their local records
5. Set SyncManager state to IDLE
6. Optionally: set is_dirty = 1 on all records so if they log back in,
   any changes made while logged out are pushed
```

### 2.9 Login with a Different Account

Detect this case by comparing the new JWT's `account_id` claim against `sync_meta.account_id`.

```
if new_account_id != stored_account_id AND stored_account_id != null:
  → Hard-clear all local data (or prompt user)
  → Reset sync_meta
  → Run the full login flow (§2.4) for the new account
```

> This is equivalent to a fresh install from the new account's perspective.

---

## 3. Edge Case Reference

| Scenario | Expected Behaviour |
|---|---|
| App used offline after login | Writes accumulate with `is_dirty=1`. On reconnect, push runs automatically. |
| Same record edited on two devices while offline | Pull first on reconnect. LWW resolves: highest `updated_at` wins. |
| Record deleted on device A, edited on device B | If `deleted_at` timestamp > `updated_at` of edit → deletion wins. Otherwise edit wins (record is un-deleted). |
| Sync interrupted mid-push | Retry the full dirty batch. Server is idempotent on same UUID. |
| Client clock is wrong | Use `server_updated_at` from cloud records as authoritative timestamp in LWW. |
| User registers a new account (never had one) | Cloud is empty by definition. Migrate local → cloud. No dialog needed. |
| User logs out and back into the same account | Pull from `last_sync_version`. Only changes made while logged out need pushing. |
| Two devices, same account, online simultaneously | Concurrent pushes are safe — server processes per-record atomically. |
| Tombstone for a record the client never had | INSERT locally with `deleted_at` set. Safe no-op for the UI. |

---

## 4. Implementation Order (Suggested)

### Backend (do first)
1. Schema migrations (§1.1)
2. Sync version trigger (§1.2)
3. Auth endpoints (§1.4)
4. `GET /sync/summary` (§1.3)
5. `GET /sync/pull` (§1.3)
6. `POST /sync/push` (§1.3)
7. `POST /sync/migrate` (§1.3)

### Mobile App
1. Local schema migrations + `sync_meta` table (§2.1)
2. Local write hook / trigger (§2.2)
3. Device ID generation and storage
4. `SyncManager` state machine skeleton (§2.3)
5. Login flow + decision tree (§2.4)
6. Merge dialog UI (§2.5)
7. Merge algorithm (§2.6)
8. Ongoing push and pull (§2.7)
9. Logout handling (§2.8)
10. Different-account detection (§2.9)

---

## 5. Testing Checklist

```
Auth & Initial State
  ☐ Register new account with no local data → cloud stays empty, nothing crashes
  ☐ Register new account with local data → all records appear in cloud
  ☐ Login to empty account with local data → all records appear in cloud
  ☐ Login to account with data, no local data → cloud data appears locally
  ☐ Login to account with data, local data exists → merge dialog appears

Merge Dialog
  ☐ "Merge both" combines records correctly
  ☐ "Use device only" overwrites cloud
  ☐ "Use cloud only" replaces local data
  ☐ Cancel dismisses dialog and logs user out (no partial state)

Ongoing Sync
  ☐ Create record offline → appears in cloud after reconnect
  ☐ Edit record on device A → appears on device B after pull
  ☐ Delete record on device A → tombstone appears on device B
  ☐ Edit same record on two devices while offline → LWW resolves correctly

Edge Cases
  ☐ Logout preserves local data
  ☐ Login with different account clears previous account's data
  ☐ Interrupted push retries safely (no duplicates)
  ☐ Pull with since_version=0 returns full dataset
```

---

*End of specification. Feed this document to Claude Code separately for the backend project and the mobile app project. The implementer may ask for clarification on framework-specific details (ORM, HTTP client, etc.) that are intentionally left abstract here.*