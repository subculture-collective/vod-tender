# VOD Segmentation API: Design Specification

**Status**: Draft  
**Author**: Engineering Team  
**Date**: 2025-10-30  
**Related Issue**: Segmentation API design to replace 501 placeholder  
**Milestone**: Documentation & Testing

## Executive Summary

This document specifies a minimal viable segmentation API for vod-tender, enabling users to mark and retrieve temporal segments (highlights, chapters, markers) within archived VODs. The API replaces the current 501 placeholder at `/vods/{id}/segments` with a full-featured segment management system.

**Core Functionality**: Create, read, update, and delete named time-based segments for VODs  
**Primary Use Cases**: Highlight reels, chapter markers, timestamp bookmarks, clip references  
**Implementation Complexity**: Low (1-2 days backend + 1 day frontend)  
**Database Impact**: New `vod_segments` table (~200 bytes per segment)

---

## Problem Statement & Use Cases

### Current Gap

VOD archives in vod-tender lack temporal metadata beyond the raw chat timeline. Users cannot:
- Mark key moments (e.g., "opening raid", "boss kill", "funny moment")
- Create chapter navigation for long VODs
- Share timestamped references to specific segments
- Build highlight reels from marked segments

### Target Use Cases

1. **Manual Highlight Marking**: User watches a VOD and creates segments for notable moments
2. **Chapter Navigation**: Content creators define chapters for 4+ hour streams
3. **Clip Reference System**: Cross-reference segments with external clip URLs (Twitch clips, YouTube timestamps)
4. **Chat-Based Auto-Segmentation** (future): Detect segments from chat activity spikes or specific commands
5. **Export to Video Editor** (future): Generate EDL/XML from segment metadata

### Non-Goals (Out of Scope for v1)

- Automatic segment detection (ML/heuristics)
- Video transcoding or clipping (only metadata)
- Segment preview thumbnails (could be phase 2)
- Collaborative editing / multi-user segment conflicts

---

## API Design

### Endpoints

#### 1. List Segments for a VOD

**Request:**
```http
GET /vods/{id}/segments?type={type}&sort={sort}
```

**Query Parameters:**
- `type` (optional): Filter by segment type (`highlight`, `chapter`, `bookmark`, `clip`). Comma-separated for multiple types.
- `sort` (optional): Sort order. Options: `start_asc` (default), `start_desc`, `created_asc`, `created_desc`, `duration_desc`.

**Response:**
```json
{
  "vod_id": "123456789",
  "segments": [
    {
      "id": "seg_abc123",
      "type": "highlight",
      "title": "Epic boss kill",
      "start_time": 1234.5,
      "end_time": 1345.2,
      "duration": 110.7,
      "description": "Final raid boss defeated after 3 hours",
      "tags": ["raid", "boss", "victory"],
      "metadata": {
        "twitch_clip_url": "https://clips.twitch.tv/...",
        "color": "#ff5733"
      },
      "created_at": "2025-10-30T14:32:00Z",
      "updated_at": "2025-10-30T15:10:00Z"
    }
  ]
}
```

**Status Codes:**
- `200 OK`: Success
- `404 Not Found`: VOD does not exist

---

#### 2. Create a Segment

**Request:**
```http
POST /vods/{id}/segments
Content-Type: application/json

{
  "type": "highlight",
  "title": "Epic boss kill",
  "start_time": 1234.5,
  "end_time": 1345.2,
  "description": "Final raid boss defeated after 3 hours",
  "tags": ["raid", "boss", "victory"],
  "metadata": {
    "twitch_clip_url": "https://clips.twitch.tv/..."
  }
}
```

**Validation Rules:**
- `type`: Required. One of: `highlight`, `chapter`, `bookmark`, `clip`.
- `title`: Required. Max 200 characters.
- `start_time`: Required. Non-negative float (seconds from VOD start).
- `end_time`: Required. Must be > `start_time`. Max: VOD duration (if known).
- `description`: Optional. Max 2000 characters.
- `tags`: Optional. Array of strings, max 10 tags, each max 50 chars.
- `metadata`: Optional. JSON object, max 5KB serialized.

**Response:**
```json
{
  "id": "seg_abc123",
  "vod_id": "123456789",
  "type": "highlight",
  "title": "Epic boss kill",
  "start_time": 1234.5,
  "end_time": 1345.2,
  "duration": 110.7,
  "description": "Final raid boss defeated after 3 hours",
  "tags": ["raid", "boss", "victory"],
  "metadata": {
    "twitch_clip_url": "https://clips.twitch.tv/..."
  },
  "created_at": "2025-10-30T14:32:00Z",
  "updated_at": "2025-10-30T14:32:00Z"
}
```

**Status Codes:**
- `201 Created`: Segment created successfully
- `400 Bad Request`: Validation failed (invalid times, missing required fields)
- `404 Not Found`: VOD does not exist
- `422 Unprocessable Entity`: Logical error (e.g., end_time <= start_time, overlaps if strict mode enabled)

---

#### 3. Get a Single Segment

**Request:**
```http
GET /vods/{vodId}/segments/{segmentId}
```

**Response:**
Same schema as create response.

**Status Codes:**
- `200 OK`: Success
- `404 Not Found`: VOD or segment does not exist

---

#### 4. Update a Segment

**Request:**
```http
PATCH /vods/{vodId}/segments/{segmentId}
Content-Type: application/json

{
  "title": "Updated title",
  "description": "New description",
  "tags": ["updated", "tags"]
}
```

**Behavior:**
- Partial update (only provided fields are updated)
- Cannot change `vod_id` or `id`
- `updated_at` is automatically set to current timestamp
- Same validation rules as create

**Response:**
Updated segment object (same schema as create response).

**Status Codes:**
- `200 OK`: Updated successfully
- `400 Bad Request`: Validation failed
- `404 Not Found`: VOD or segment does not exist

---

#### 5. Delete a Segment

**Request:**
```http
DELETE /vods/{vodId}/segments/{segmentId}
```

**Response:**
```http
204 No Content
```

**Status Codes:**
- `204 No Content`: Deleted successfully
- `404 Not Found`: VOD or segment does not exist

---

#### 6. Bulk Operations (Optional - Phase 2)

Future endpoints for efficiency:

```http
POST /vods/{id}/segments/bulk
DELETE /vods/{id}/segments/bulk
```

Payload: Array of segment operations. Returns array of results with per-item status.

---

## Database Schema

### New Table: `vod_segments`

```sql
CREATE TABLE vod_segments (
    id TEXT PRIMARY KEY DEFAULT ('seg_' || encode(gen_random_bytes(12), 'hex')),
    vod_id TEXT NOT NULL REFERENCES vods(twitch_vod_id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('highlight', 'chapter', 'bookmark', 'clip')),
    title TEXT NOT NULL,
    start_time DOUBLE PRECISION NOT NULL CHECK (start_time >= 0),
    end_time DOUBLE PRECISION NOT NULL CHECK (end_time > start_time),
    duration DOUBLE PRECISION GENERATED ALWAYS AS (end_time - start_time) STORED,
    description TEXT,
    tags TEXT[], -- PostgreSQL array for efficient querying
    metadata JSONB, -- Flexible key-value store for extensions
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    channel TEXT NOT NULL DEFAULT '', -- Multi-channel support
    
    -- Composite index for efficient VOD segment listing
    INDEX idx_vod_segments_vod_id (vod_id, start_time),
    INDEX idx_vod_segments_type (vod_id, type),
    INDEX idx_vod_segments_tags USING GIN (tags)
);
```

**Storage Estimates:**
- ~180 bytes base + variable (title, description, tags, metadata)
- 100 segments/VOD = ~20KB per VOD
- 1000 VODs with segments = ~20MB (negligible)

**Cascade Behavior:**
- When a VOD is deleted, all associated segments are automatically deleted (`ON DELETE CASCADE`)

---

## Implementation Phases

### Phase 1: Core CRUD (MVP) - 2 Days

**Backend (1.5 days):**
1. Create `vod_segments` table migration in `backend/db/db.go`
2. Implement CRUD handlers in `backend/server/server.go`:
   - Replace `handleVodSegments` placeholder with routing logic
   - Create handlers: `handleSegmentsList`, `handleSegmentCreate`, `handleSegmentGet`, `handleSegmentUpdate`, `handleSegmentDelete`
3. Add validation logic (time bounds, type enum, length limits)
4. Update OpenAPI spec in `backend/api/openapi.yaml`
5. Add unit tests (mock DB interactions)

**Frontend (0.5 days):**
1. Create segment list component in frontend (read-only view)
2. Add "View Segments" button to VOD detail page
3. Display segments as timeline overlay or list view

**Acceptance Criteria:**
- [ ] All CRUD endpoints functional and tested
- [ ] OpenAPI spec updated with full schemas
- [ ] Basic UI can display segments for a VOD
- [ ] Manual testing: Create segment via cURL, verify in frontend

---

### Phase 2: Advanced Features - 1-2 Days (Future)

**Timeline Visualization:**
- Interactive timeline component showing segments as color-coded bars
- Click segment to jump to that timestamp in VOD player

**Search & Filtering:**
- Full-text search on titles/descriptions
- Tag-based filtering
- Date range filtering

**Bulk Operations:**
- Import segments from JSON/CSV
- Export segments to EDL format (video editing)

**Auto-Segmentation Hooks:**
- Webhook/event system to trigger segmentation on VOD processing complete
- Integration point for ML models (future)

---

### Phase 3: Integration & Optimization - 1 Day (Future)

**Chat Integration:**
- Show chat activity graph alongside segments
- "Create segment from chat spike" feature

**Thumbnail Preview:**
- Generate thumbnail images for segment start times (requires ffmpeg/imagemagick)
- Store thumbnail references in `metadata` JSONB field

**Performance Optimization:**
- Add caching layer (Redis) for frequently accessed segments
- Paginate segment lists for VODs with 100+ segments

**Analytics:**
- Track segment view counts
- Popular segments dashboard

---

## API Client Examples

### Example 1: Create a Chapter Segment (cURL)

```bash
curl -X POST http://localhost:8080/vods/123456789/segments \
  -H "Content-Type: application/json" \
  -d '{
    "type": "chapter",
    "title": "Intro & Setup",
    "start_time": 0,
    "end_time": 180.5,
    "description": "Stream starting soon screen and intro music"
  }'
```

### Example 2: List All Highlights (JavaScript)

```javascript
const response = await fetch(
  'http://localhost:8080/vods/123456789/segments?type=highlight&sort=duration_desc'
);
const data = await response.json();
console.log(`Found ${data.segments.length} highlights`);
data.segments.forEach(seg => {
  console.log(`${seg.title}: ${seg.start_time}s - ${seg.end_time}s`);
});
```

### Example 3: Update Segment Tags (Python)

```python
import requests

response = requests.patch(
    'http://localhost:8080/vods/123456789/segments/seg_abc123',
    json={
        'tags': ['raid', 'boss', 'victory', 'world-first']
    }
)
print(f"Updated segment: {response.json()['title']}")
```

---

## Security & Authorization

### Phase 1 (MVP): No Authentication
- All endpoints publicly accessible (consistent with current VOD API)
- Rely on network-level security (firewall, VPN)

### Phase 2 (Future): Optional Authentication
- Add `Authorization` header support (Bearer token)
- Implement rate limiting per IP (already exists in middleware)
- Admin-only delete operations (configurable via `ADMIN_TOKEN`)

### Abuse Prevention
- Rate limit: 10 POST/DELETE requests per minute per IP (existing middleware)
- Max 100 segments per VOD (enforced in handler)
- Max 2KB payload size per segment (nginx/middleware)

---

## Monitoring & Observability

### Metrics (Prometheus)

Add to `backend/telemetry/metrics.go`:

```go
var (
    segmentCreations = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "vod_segments_created_total",
            Help: "Total segments created by type",
        },
        []string{"type"},
    )
    
    segmentQueries = promauto.NewHistogram(
        prometheus.HistogramOpts{
            Name: "vod_segments_query_duration_seconds",
            Help: "Segment list query duration",
            Buckets: prometheus.DefBuckets,
        },
    )
)
```

### Logging

Structured logs with correlation IDs (existing pattern):

```go
slog.Info("segment created",
    "corr", corrID,
    "vod_id", vodID,
    "segment_id", segID,
    "type", segType,
    "duration", duration,
)
```

---

## Migration & Rollback

### Deploying Phase 1

1. **Database Migration**: Run on startup (existing `db.Migrate()` pattern)
   ```sql
   CREATE TABLE IF NOT EXISTS vod_segments (...);
   ```

2. **Code Deployment**: Blue-green or rolling update (no breaking changes)

3. **Verification**:
   ```bash
   # Test health endpoint
   curl http://localhost:8080/healthz
   
   # Test segments endpoint
   curl http://localhost:8080/vods/123456789/segments
   # Expected: {"vod_id": "123456789", "segments": []}
   ```

### Rollback Plan

If critical bugs discovered:

1. **Code Rollback**: Revert to previous version (segment endpoints return 501 again)
2. **Data Preservation**: Do NOT drop `vod_segments` table (segments persist for future re-deployment)
3. **Clean Rollback** (if needed):
   ```sql
   DROP TABLE IF EXISTS vod_segments CASCADE;
   ```

---

## Open Questions & Future Considerations

### 1. Should segments allow overlaps?

**MVP Decision**: Yes, allow overlaps (e.g., multiple highlights can span same time range)  
**Future**: Add optional `overlap_policy` flag (strict mode rejects overlaps)

### 2. How to handle VOD duration unknown?

**MVP Decision**: Skip validation if `duration_seconds` is NULL in `vods` table  
**Future**: Fetch duration from Twitch API or yt-dlp metadata

### 3. Segment permissions (multi-channel)?

**MVP Decision**: Inherit channel from VOD (via `vod_id` foreign key)  
**Future**: Per-segment `channel` column (redundant but faster queries)

### 4. Export formats?

**Phase 2 Candidates**:
- EDL (Edit Decision List) for video editors
- WebVTT chapters for web players
- JSON for API consumers

### 5. Automatic segment detection?

**Phase 3+**: Potential integrations:
- Chat activity spikes (via `chat_messages` table)
- Audio analysis (scene change detection)
- ML models (trained on labeled segments)

---

## Success Metrics

### Phase 1 (MVP)
- [ ] API endpoints return 200-series status codes (not 501)
- [ ] OpenAPI spec validates without errors
- [ ] Can create/read/update/delete segments via cURL
- [ ] Frontend displays segment list for a VOD
- [ ] Backend tests achieve >80% coverage for segment handlers

### Phase 2 (Advanced)
- [ ] Timeline visualization renders for VODs with 10+ segments
- [ ] Users can export segments to EDL format
- [ ] Bulk import tested with 100+ segments (< 2s response time)

### Phase 3 (Production)
- [ ] Average segment creation latency < 50ms (p95)
- [ ] Segment queries cached (80%+ cache hit rate)
- [ ] Zero downtime deployments during segment migrations

---

## References

- **Current Placeholder**: `backend/server/server.go:handleVodSegments()`
- **OpenAPI Spec**: `backend/api/openapi.yaml` (lines 129-138)
- **Related Docs**:
  - `docs/ARCHITECTURE.md` - Overall system design
  - `docs/CONFIG.md` - Environment variables
  - `docs/OPERATIONS.md` - Deployment & monitoring

---

## Appendix: Database Migration SQL

**File**: `backend/db/db.go` (add to `schemaStatements` array)

```sql
-- VOD Segmentation table
CREATE TABLE IF NOT EXISTS vod_segments (
    id TEXT PRIMARY KEY DEFAULT ('seg_' || encode(gen_random_bytes(12), 'hex')),
    vod_id TEXT NOT NULL REFERENCES vods(twitch_vod_id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('highlight', 'chapter', 'bookmark', 'clip')),
    title TEXT NOT NULL,
    start_time DOUBLE PRECISION NOT NULL CHECK (start_time >= 0),
    end_time DOUBLE PRECISION NOT NULL CHECK (end_time > start_time),
    duration DOUBLE PRECISION GENERATED ALWAYS AS (end_time - start_time) STORED,
    description TEXT,
    tags TEXT[],
    metadata JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    channel TEXT NOT NULL DEFAULT ''
);

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_vod_segments_vod_id ON vod_segments(vod_id, start_time);
CREATE INDEX IF NOT EXISTS idx_vod_segments_type ON vod_segments(vod_id, type);
CREATE INDEX IF NOT EXISTS idx_vod_segments_tags ON vod_segments USING GIN(tags);

-- Multi-channel support (add column if exists)
ALTER TABLE vod_segments ADD COLUMN IF NOT EXISTS channel TEXT NOT NULL DEFAULT '';
```

---

**Document Status**: Draft for review  
**Next Steps**: Team review → Implementation phase 1 → Iterate based on feedback
