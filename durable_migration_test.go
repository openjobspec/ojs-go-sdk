package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// legacyReplayFixture is the exact replay log an older SDK would have written:
// a JSON array serialised into a *string* under metadata._replay_log.
func legacyReplayFixture() string {
	return `[` +
		`{"seq":0,"type":"time","key":"now","result":"2026-01-15T10:00:00Z"},` +
		`{"seq":1,"type":"random","result":"deadbeef"},` +
		`{"seq":2,"type":"call","key":"api-call","result":{"price":99.99}}` +
		`]`
}

// assertFixtureReplays replays the fixture through dc and asserts that every
// recorded side effect is reproduced exactly, with no live execution.
func assertFixtureReplays(t *testing.T, dc *DurableContext) {
	t.Helper()

	if !dc.IsReplaying() {
		t.Fatal("a recovered legacy checkpoint must put the context in replay mode")
	}
	if got := dc.Now(); got.Year() != 2026 || got.Month() != 1 || got.Day() != 15 {
		t.Errorf("replayed time = %v, want 2026-01-15", got)
	}
	if got := dc.Random(4); got != "deadbeef" {
		t.Errorf("replayed random = %q, want deadbeef", got)
	}
	result, err := dc.SideEffect("api-call", func() (any, error) {
		t.Error("legacy replay must not re-execute a recorded side effect")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("SideEffect replay: %v", err)
	}
	var price map[string]float64
	if err := json.Unmarshal(result, &price); err != nil {
		t.Fatalf("decode replayed result: %v", err)
	}
	if price["price"] != 99.99 {
		t.Errorf("replayed result = %v, want 99.99", price)
	}
	if dc.IsReplaying() {
		t.Error("replay should be exhausted after the third entry")
	}
}

// legacyServer serves the pre-standard resume endpoint and records the
// canonical checkpoint written back by the migration.
type legacyServer struct {
	mu sync.Mutex

	// resume is the body returned by the legacy resume endpoint.
	resume any
	// resumeStatus, when non-zero, is returned instead of resume.
	resumeStatus int
	// canonical, when non-nil, is returned by the standard checkpoint GET.
	canonical any
	// migrateStatus, when non-zero, fails the canonical write.
	migrateStatus int

	migrated  []checkpointRequest
	legacyHit int
}

func (s *legacyServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		s.mu.Lock()
		defer s.mu.Unlock()

		switch {
		case r.URL.EscapedPath() == durableCheckpointPath("job-legacy") && r.Method == http.MethodGet:
			if s.canonical == nil {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "no checkpoint"})
				return
			}
			_ = json.NewEncoder(w).Encode(s.canonical)

		case r.URL.EscapedPath() == durableCheckpointPath("job-legacy") && r.Method == http.MethodPost:
			if s.migrateStatus != 0 {
				w.WriteHeader(s.migrateStatus)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "write rejected"})
				return
			}
			var req checkpointRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode migrated checkpoint: %v", err)
			}
			s.migrated = append(s.migrated, req)
			_ = json.NewEncoder(w).Encode(map[string]any{"checkpoint": map[string]any{"sequence": 1}})

		case r.URL.EscapedPath() == durableLegacyCheckpointPath("job-legacy"):
			s.legacyHit++
			if s.resumeStatus != 0 {
				w.WriteHeader(s.resumeStatus)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "legacy failure"})
				return
			}
			_ = json.NewEncoder(w).Encode(s.resume)

		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	}
}

func (s *legacyServer) migratedCheckpoints() []checkpointRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]checkpointRequest(nil), s.migrated...)
}

// TestDurableLegacyEndpointCheckpointIsReplayedAndMigrated is the core
// migration case: the job was checkpointed by an older SDK at the legacy
// endpoint, the standard resource reports 404, and the SDK must still replay
// every recorded side effect — then write the canonical v1 state forward.
func TestDurableLegacyEndpointCheckpointIsReplayedAndMigrated(t *testing.T) {
	s := &legacyServer{resume: map[string]any{
		"has_checkpoint": true,
		"checkpoint": map[string]any{
			"step_index": 2,
			"state":      map[string]any{"phase": "transform"},
			"metadata": map[string]string{
				legacyReplayLogKey: legacyReplayFixture(),
				"attempt":          "1",
			},
		},
	}}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 2)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}
	if mErr := dc.MigrationError(); mErr != nil {
		t.Fatalf("MigrationError() = %v, want nil", mErr)
	}

	migrated := s.migratedCheckpoints()
	if len(migrated) != 1 {
		t.Fatalf("migrated checkpoints = %d, want 1 canonical write", len(migrated))
	}
	got := migrated[0].State
	if got.Version != durableCheckpointVersion {
		t.Errorf("migrated version = %d, want %d", got.Version, durableCheckpointVersion)
	}
	if got.StepIndex != 2 {
		t.Errorf("migrated step_index = %d, want 2 (preserved from the legacy checkpoint)", got.StepIndex)
	}
	if got.Attempt != 2 {
		t.Errorf("migrated attempt = %d, want 2 (the current attempt)", got.Attempt)
	}
	if string(got.State) != `{"phase":"transform"}` {
		t.Errorf("migrated state = %s, want the legacy caller state verbatim", got.State)
	}
	if len(got.ReplayLog) != 3 {
		t.Fatalf("migrated replay log = %+v, want the 3 legacy entries", got.ReplayLog)
	}
	for i, e := range got.ReplayLog {
		if e.Seq != i {
			t.Errorf("migrated entry %d has seq %d", i, e.Seq)
		}
	}

	assertFixtureReplays(t, dc)
}

// TestDurableLegacyEncodedStandardCheckpointIsReplayedAndMigrated covers the
// other legacy shape: the standard endpoint answers, but the state it holds was
// written with the unversioned encoding (version 0, metadata._replay_log).
func TestDurableLegacyEncodedStandardCheckpointIsReplayedAndMigrated(t *testing.T) {
	s := &legacyServer{canonical: map[string]any{
		"checkpoint": map[string]any{
			"state": map[string]any{
				"step_index": 7,
				"state":      map[string]any{"phase": "load"},
				"metadata": map[string]string{
					legacyReplayLogKey: legacyReplayFixture(),
				},
			},
		},
	}}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 3)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}
	if mErr := dc.MigrationError(); mErr != nil {
		t.Fatalf("MigrationError() = %v, want nil", mErr)
	}
	if s.legacyHit != 0 {
		t.Errorf("legacy endpoint was polled %d times; the standard resource answered", s.legacyHit)
	}

	migrated := s.migratedCheckpoints()
	if len(migrated) != 1 {
		t.Fatalf("migrated checkpoints = %d, want 1", len(migrated))
	}
	if migrated[0].State.Version != durableCheckpointVersion || migrated[0].State.StepIndex != 7 {
		t.Errorf("migrated state = %+v, want v%d at step 7", migrated[0].State, durableCheckpointVersion)
	}

	assertFixtureReplays(t, dc)
}

// TestDurableCanonicalCheckpointIsNotMigrated keeps the common path free of
// extra writes: a v1 checkpoint is already canonical.
func TestDurableCanonicalCheckpointIsNotMigrated(t *testing.T) {
	logJSON := json.RawMessage(legacyReplayFixture())
	s := &legacyServer{canonical: map[string]any{
		"checkpoint": map[string]any{
			"state": map[string]any{
				"ojs_go_durable_version": durableCheckpointVersion,
				"state":                  nil,
				"step_index":             2,
				"replay_log":             logJSON,
				"attempt":                1,
			},
		},
	}}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 2)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}
	if got := s.migratedCheckpoints(); len(got) != 0 {
		t.Fatalf("migrated %d checkpoints, want none for an already-canonical state", len(got))
	}
	if s.legacyHit != 0 {
		t.Errorf("legacy endpoint polled %d times, want 0", s.legacyHit)
	}
	assertFixtureReplays(t, dc)
}

// TestDurableLegacyEndpoint404MeansNoCheckpoint keeps the documented
// no-checkpoint result intact when neither resource holds anything.
func TestDurableLegacyEndpoint404MeansNoCheckpoint(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		s := &legacyServer{resumeStatus: status}
		srv := httptest.NewServer(s.handler(t))

		tp := newTransport(srv.URL, clientConfig{})
		dc, err := newDurableContext(context.Background(), tp, "job-legacy", 1)
		if err != nil {
			srv.Close()
			t.Fatalf("legacy status %d: newDurableContext = %v, want no error", status, err)
		}
		if dc.IsReplaying() {
			srv.Close()
			t.Fatalf("legacy status %d: context entered replay mode with no checkpoint", status)
		}
		if s.legacyHit != 1 {
			srv.Close()
			t.Fatalf("legacy status %d: legacy endpoint hit %d times, want 1", status, s.legacyHit)
		}
		srv.Close()
	}
}

// TestDurableLegacyHasCheckpointFalseMeansNoCheckpoint covers the legacy
// endpoint's own "nothing stored" answer.
func TestDurableLegacyHasCheckpointFalseMeansNoCheckpoint(t *testing.T) {
	s := &legacyServer{resume: map[string]any{"has_checkpoint": false}}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 1)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}
	if dc.IsReplaying() {
		t.Error("has_checkpoint=false must not enter replay mode")
	}
	if got := s.migratedCheckpoints(); len(got) != 0 {
		t.Errorf("migrated %d checkpoints, want none", len(got))
	}
}

// TestDurableLegacyCheckpointWithoutReplayLogIsNotAdopted keeps a legacy
// checkpoint that carries no SDK replay log out of the replay path.
func TestDurableLegacyCheckpointWithoutReplayLogIsNotAdopted(t *testing.T) {
	s := &legacyServer{resume: map[string]any{
		"has_checkpoint": true,
		"checkpoint": map[string]any{
			"step_index": 1,
			"metadata":   map[string]string{"attempt": "1"},
		},
	}}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 1)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}
	if dc.IsReplaying() {
		t.Error("a legacy checkpoint with no replay log has nothing to replay")
	}
	if got := s.migratedCheckpoints(); len(got) != 0 {
		t.Errorf("migrated %d checkpoints, want none", len(got))
	}
}

// TestDurableLegacyEndpointErrorStopsTheHandler locks the integrity rule: a
// legacy endpoint that fails is not the same as "no checkpoint", because
// proceeding would re-run recorded side effects.
func TestDurableLegacyEndpointErrorStopsTheHandler(t *testing.T) {
	s := &legacyServer{resumeStatus: http.StatusInternalServerError}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 1)
	if err == nil {
		t.Fatal("expected a legacy checkpoint load error")
	}
	if dc != nil {
		t.Fatalf("newDurableContext returned %#v after a load failure", dc)
	}
	if got := err.Error(); !strings.Contains(got, `load legacy durable checkpoint for job "job-legacy"`) {
		t.Fatalf("error = %q, want the legacy load context", got)
	}
}

// TestDurableLegacyReplayLogCorruptionStopsTheHandler covers an undecodable
// legacy log: replay cannot be guaranteed, so the handler must not run.
func TestDurableLegacyReplayLogCorruptionStopsTheHandler(t *testing.T) {
	s := &legacyServer{resume: map[string]any{
		"has_checkpoint": true,
		"checkpoint": map[string]any{
			"metadata": map[string]string{legacyReplayLogKey: `{"not":"an array"}`},
		},
	}}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 1)
	if err == nil || !strings.Contains(err.Error(), "decode legacy durable replay log") {
		t.Fatalf("newDurableContext error = %v, want a legacy replay-log decode failure", err)
	}
	if dc != nil {
		t.Fatalf("newDurableContext returned %#v for a corrupt legacy log", dc)
	}
}

// TestDurableLegacyReplayLogSequenceIsValidated applies the canonical
// sequence-integrity rule to recovered legacy logs as well.
func TestDurableLegacyReplayLogSequenceIsValidated(t *testing.T) {
	s := &legacyServer{resume: map[string]any{
		"has_checkpoint": true,
		"checkpoint": map[string]any{
			"metadata": map[string]string{
				legacyReplayLogKey: `[{"seq":3,"type":"time","result":"2026-01-15T10:00:00Z"}]`,
			},
		},
	}}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	_, err := newDurableContext(context.Background(), tp, "job-legacy", 1)
	if err == nil || !strings.Contains(err.Error(), "has sequence 3 at position 0") {
		t.Fatalf("newDurableContext error = %v, want a sequence-integrity failure", err)
	}
}

// TestDurableLegacyMigrationFailureDoesNotBreakReplay locks the priority: a
// failed forward-write is diagnostic, never a reason to stop replaying a log
// that was recovered successfully.
func TestDurableLegacyMigrationFailureDoesNotBreakReplay(t *testing.T) {
	s := &legacyServer{
		migrateStatus: http.StatusServiceUnavailable,
		resume: map[string]any{
			"has_checkpoint": true,
			"checkpoint": map[string]any{
				"step_index": 1,
				"metadata":   map[string]string{legacyReplayLogKey: legacyReplayFixture()},
			},
		},
	}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 1)
	if err != nil {
		t.Fatalf("newDurableContext = %v; a migration failure must not stop the handler", err)
	}
	if dc.MigrationError() == nil {
		t.Error("a failed migration must be reported by MigrationError, not swallowed")
	}
	assertFixtureReplays(t, dc)
}

// TestDurableLegacyCheckpointPathEscapesJobID mirrors the canonical path rule.
func TestDurableLegacyCheckpointPathEscapesJobID(t *testing.T) {
	got := durableLegacyCheckpointPath("job/with?reserved")
	want := "/ojs/v1/checkpoints/job%2Fwith%3Freserved/resume"
	if got != want {
		t.Fatalf("durableLegacyCheckpointPath = %q, want %q", got, want)
	}
}

// TestDurableStandardCheckpointIsPreferredOverLegacy proves the legacy endpoint
// is a fallback, not a second source of truth.
func TestDurableStandardCheckpointIsPreferredOverLegacy(t *testing.T) {
	s := &legacyServer{
		canonical: map[string]any{
			"checkpoint": map[string]any{
				"state": map[string]any{
					"ojs_go_durable_version": durableCheckpointVersion,
					"step_index":             1,
					"replay_log":             json.RawMessage(`[{"seq":0,"type":"random","result":"c0ffee"}]`),
					"attempt":                1,
				},
			},
		},
		resume: map[string]any{
			"has_checkpoint": true,
			"checkpoint": map[string]any{
				"metadata": map[string]string{legacyReplayLogKey: legacyReplayFixture()},
			},
		},
	}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	tp := newTransport(srv.URL, clientConfig{})
	dc, err := newDurableContext(context.Background(), tp, "job-legacy", 1)
	if err != nil {
		t.Fatalf("newDurableContext: %v", err)
	}
	if got := dc.Random(3); got != "c0ffee" {
		t.Errorf("replayed random = %q, want the standard checkpoint's c0ffee", got)
	}
	if s.legacyHit != 0 {
		t.Errorf("legacy endpoint hit %d times while a standard checkpoint existed", s.legacyHit)
	}
}
