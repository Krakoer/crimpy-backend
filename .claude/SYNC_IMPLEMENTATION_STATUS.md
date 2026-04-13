# Cloud Sync Implementation Status

## Completed

### Backend Schema Changes
- ✅ Modified `schema/schema.sql` to add cloud sync fields to all tables:
  - Changed all table IDs from `SERIAL` to `UUID`
  - Added `created_at`, `updated_at`, `deleted_at` timestamps
  - Added `device_id`, `sync_version`, `server_updated_at` for sync tracking
  - Added `user_id` foreign key to all syncable tables
  - Added `last_seen_at` to users table
  - Created indexes for efficient sync queries

### Database Migrations
- ✅ Created migration `20260413095350_add_cloud_sync_support.sql`
  - Converts all SERIAL IDs to UUIDs
  - Adds sync columns to all tables
  - Drops old sequences
  - Creates sync indexes

- ✅ Created migration `20260413095400_add_sync_triggers.sql`
  - Implements `bump_sync_version()` trigger function
  - Auto-updates `server_updated_at` and `sync_version` on every row write
  - Auto-updates `updated_at` timestamp
  - Applies triggers to all synced tables

### Sync Queries
- ✅ Created `queries/sync.sql` with all sync operations:
  - `GetSyncSummary` - Returns record counts per collection
  - `GetXXXSinceVersion` - Incremental pull for each table
  - `GetXXXByID` - Fetch individual records
  - `UpsertXXX` - Last-write-wins upsert for each table
  - `UpdateUserLastSeen` - Track user activity

### Sync Handler
- ✅ Created `internal/handler/sync.go` with sync endpoints:
  - `GET /api/sync/summary` - Returns collection counts and last sync version
  - `GET /api/sync/pull?since_version=N` - Incremental pull
  - `POST /api/sync/push` - Push local changes (stub implementation)
  - `POST /api/sync/migrate` - Bulk upload on first sync (stub implementation)

### Routes
- ✅ Added sync routes to `cmd/api/main.go`
- ✅ All sync endpoints protected by JWT authentication

## Completed - Handler UUID Migration

### Handler Refactoring - DONE
- ✅ Updated session.go to use UUID string parameters
- ✅ Updated training.go to use UUID string parameters
- ✅ Updated repeater.go to use UUID string parameters
- ✅ Fixed sync.go type assertions for LastSyncVersion
- ✅ Added UUID conversion helper functions (uuidToString, uuidPtrToString)
- ✅ Updated all Swagger documentation to reflect UUID parameters
- ✅ Changed CreateTrainingRequest.RepeaterID from int32 to string
- ✅ Regenerated database code with sqlc
- ✅ Build successful with all UUID changes

### UUID Implementation Details
All handlers now use:
```go
idStr := c.Params("id")
var uuid pgtype.UUID
uuid.Scan(idStr)
h.queries.GetSession(ctx, uuid)
```

User ownership verification uses byte comparison:
```go
if !isAdmin && resource.UserID.Bytes != userUUID.Bytes {
    return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
}
```

## In Progress

### Schema Applied Successfully
- ✅ Removed Atlas trigger functions (Atlas Pro-only feature)
- ✅ Sync version and server_updated_at will be managed in application code
- ✅ All tests passing with UUID-based schema
- ✅ Fixed test utilities to handle UUIDs instead of integers

### Sync Handler - Push/Migrate Implementation
The Push and Migrate handlers currently return stub responses. Need to implement:

1. **Push endpoint** - Parse incoming records and call Upsert queries
2. **Migrate endpoint** - Bulk insert with conflict handling
3. **Proper error handling** for malformed UUIDs and timestamps
4. **Validation** of device_id header

## Pending

### Missing Features

#### Device ID Middleware
Need to add middleware to extract and validate `X-Device-ID` header on sync endpoints.

#### Swagger Documentation
Add Swagger annotations to all sync endpoints.

#### Integration Tests
Create tests for:
- Sync summary with empty/populated data
- Pull with various since_version values
- Push with valid/invalid records
- Migrate with bulk data
- Conflict resolution (LWW based on updated_at)

## Known Issues

### 1. Fiber v3 API Changes
```go
// ERROR: c.QueryInt undefined
sinceVersion := c.QueryInt("since_version", 0)

// FIX: Use Query() and parse manually
sinceVersionStr := c.Query("since_version", "0")
sinceVersion, _ := strconv.ParseInt(sinceVersionStr, 10, 64)
```

### 2. Type Assertion for GetSyncSummary
The summary query returns an anonymous struct. Need to check the generated type in `sync.sql.go`.

### 3. UUID String Conversion
UUIDs are stored as `pgtype.UUID` with `.Bytes` field. Need helper function:
```go
func uuidToString(u pgtype.UUID) string {
    if !u.Valid {
        return ""
    }
    // Use proper UUID formatting
    return uuid.UUID(u.Bytes).String()
}
```

## Next Steps

### Priority 1: Make the Build Work
1. Fix Fiber v3 API usage in sync.go
2. Fix type assertion for GetSyncSummary
3. Update all existing handlers to use UUIDs
4. Update all existing queries to use UUID parameters
5. Run `sqlc generate` and fix any remaining errors

### Priority 2: Complete Sync Implementation
1. Implement Push handler with proper record parsing
2. Implement Migrate handler
3. Add device ID middleware/validation
4. Add proper error responses

### Priority 3: Testing
1. Apply migrations to dev database
2. Create integration tests
3. Test with Bruno/Postman
4. Verify trigger functions work correctly

### Priority 4: Documentation
1. Add Swagger annotations
2. Update API documentation
3. Document breaking changes for mobile app team

## Migration Strategy for Production

**WARNING:** This is a breaking schema change. Production deployment requires:

1. **Backup database** before migration
2. **Coordinate with mobile app** - they need to update simultaneously
3. **Consider blue-green deployment** or maintenance window
4. **Data migration** - existing integer IDs will be converted to UUIDs

Alternatively, consider:
- Creating new UUID-based tables alongside old ones
- Gradual migration with dual-write period
- Versioned API endpoints (/v1 vs /v2)
