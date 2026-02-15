package ojstesting

import (
	"errors"
	"testing"
)

func TestFakeActivatesStore(t *testing.T) {
	s := Fake(t)
	if s == nil {
		t.Fatal("Fake() returned nil")
	}

	got := GetStore()
	if got != s {
		t.Fatal("GetStore() did not return the active store")
	}
}

func TestFakeCleanup(t *testing.T) {
	// Run a sub-test with Fake, then verify cleanup.
	var inner *FakeStore
	t.Run("inner", func(t *testing.T) {
		inner = Fake(t)
		if inner == nil {
			t.Fatal("Fake() returned nil")
		}
	})

	// After the sub-test completes, cleanup should have run.
	got := GetStore()
	if got == inner {
		t.Error("expected store to be cleaned up after sub-test")
	}
}

func TestRecordEnqueue(t *testing.T) {
	s := Fake(t)

	job := s.RecordEnqueue("email.send", []any{map[string]any{"to": "a@b.com"}}, "email", nil)
	if job.ID == "" {
		t.Error("expected job ID to be set")
	}
	if job.Type != "email.send" {
		t.Errorf("expected type email.send, got %s", job.Type)
	}
	if job.Queue != "email" {
		t.Errorf("expected queue email, got %s", job.Queue)
	}
	if job.State != "available" {
		t.Errorf("expected state available, got %s", job.State)
	}
}

func TestRecordEnqueueDefaultQueue(t *testing.T) {
	s := Fake(t)

	job := s.RecordEnqueue("test.job", nil, "", nil)
	if job.Queue != "default" {
		t.Errorf("expected default queue, got %s", job.Queue)
	}
}

func TestAssertEnqueued(t *testing.T) {
	Fake(t)

	store := GetStore()
	store.RecordEnqueue("email.send", []any{map[string]any{"to": "a@b.com"}}, "email", nil)
	store.RecordEnqueue("sms.send", []any{map[string]any{"number": "123"}}, "default", nil)

	// Should pass: email.send was enqueued.
	AssertEnqueued(t, "email.send")

	// Should pass: sms.send was enqueued.
	AssertEnqueued(t, "sms.send")
}

func TestAssertEnqueuedWithMatchQueue(t *testing.T) {
	Fake(t)

	store := GetStore()
	store.RecordEnqueue("email.send", nil, "email", nil)
	store.RecordEnqueue("email.send", nil, "priority", nil)

	AssertEnqueued(t, "email.send", MatchQueue("email"))
	AssertEnqueued(t, "email.send", MatchQueue("priority"))
}

func TestAssertEnqueuedWithMatchCount(t *testing.T) {
	Fake(t)

	store := GetStore()
	store.RecordEnqueue("email.send", nil, "default", nil)
	store.RecordEnqueue("email.send", nil, "default", nil)
	store.RecordEnqueue("email.send", nil, "default", nil)

	AssertEnqueued(t, "email.send", MatchCount(3))
}

func TestAssertEnqueuedWithMatchArgs(t *testing.T) {
	Fake(t)

	store := GetStore()
	store.RecordEnqueue("email.send", []any{map[string]any{"to": "user@example.com"}}, "default", nil)

	AssertEnqueued(t, "email.send", MatchArgs(map[string]any{"to": "user@example.com"}))
}

func TestAssertEnqueuedWithMatchMeta(t *testing.T) {
	Fake(t)

	store := GetStore()
	store.RecordEnqueue("email.send", nil, "default", map[string]any{"source": "api", "version": "2"})

	AssertEnqueued(t, "email.send", MatchMeta(map[string]any{"source": "api"}))
}

func TestRefuteEnqueued(t *testing.T) {
	Fake(t)

	store := GetStore()
	store.RecordEnqueue("email.send", nil, "default", nil)

	// sms.send was NOT enqueued, so this should pass.
	RefuteEnqueued(t, "sms.send")
}

func TestRefuteEnqueuedWithQueue(t *testing.T) {
	Fake(t)

	store := GetStore()
	store.RecordEnqueue("email.send", nil, "email", nil)

	// email.send was enqueued in "email" queue, not "priority".
	RefuteEnqueued(t, "email.send", MatchQueue("priority"))
}

func TestAllEnqueued(t *testing.T) {
	Fake(t)

	store := GetStore()
	store.RecordEnqueue("email.send", nil, "default", nil)
	store.RecordEnqueue("sms.send", nil, "default", nil)
	store.RecordEnqueue("email.send", nil, "default", nil)

	all := AllEnqueued()
	if len(all) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(all))
	}

	emails := AllEnqueued("email.send")
	if len(emails) != 2 {
		t.Fatalf("expected 2 email.send jobs, got %d", len(emails))
	}

	sms := AllEnqueued("sms.send")
	if len(sms) != 1 {
		t.Fatalf("expected 1 sms.send job, got %d", len(sms))
	}
}

func TestAllEnqueuedWithoutFake(t *testing.T) {
	// When not in fake mode, AllEnqueued should return nil.
	storeMu.Lock()
	old := activeStore
	activeStore = nil
	storeMu.Unlock()
	defer func() {
		storeMu.Lock()
		activeStore = old
		storeMu.Unlock()
	}()

	result := AllEnqueued()
	if result != nil {
		t.Error("expected nil when not in fake mode")
	}
}

func TestClearAll(t *testing.T) {
	Fake(t)

	store := GetStore()
	store.RecordEnqueue("email.send", nil, "default", nil)

	ClearAll(t)

	all := AllEnqueued()
	if len(all) != 0 {
		t.Fatalf("expected 0 after ClearAll, got %d", len(all))
	}
}

func TestDrain(t *testing.T) {
	s := Fake(t)

	s.RecordEnqueue("email.send", nil, "default", nil)
	s.RecordEnqueue("sms.send", nil, "default", nil)

	Drain(t)

	AssertPerformed(t, "email.send")
	AssertPerformed(t, "sms.send")
}

func TestDrainWithHandler(t *testing.T) {
	s := Fake(t)

	s.RegisterHandler("test.fail", func(job FakeJob) error {
		return errors.New("processing failed")
	})

	s.RecordEnqueue("test.fail", nil, "default", nil)

	Drain(t)

	// The job should have been performed but marked as discarded.
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.performed) != 1 {
		t.Fatalf("expected 1 performed job, got %d", len(s.performed))
	}
	if s.performed[0].State != "discarded" {
		t.Errorf("expected state discarded, got %s", s.performed[0].State)
	}
}

func TestDrainWithSuccessHandler(t *testing.T) {
	s := Fake(t)

	s.RegisterHandler("test.ok", func(job FakeJob) error {
		return nil
	})

	s.RecordEnqueue("test.ok", nil, "default", nil)

	Drain(t)

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.performed) != 1 {
		t.Fatalf("expected 1 performed job, got %d", len(s.performed))
	}
	if s.performed[0].State != "completed" {
		t.Errorf("expected state completed, got %s", s.performed[0].State)
	}
}

func TestUniqueJobIDs(t *testing.T) {
	s := Fake(t)

	j1 := s.RecordEnqueue("a", nil, "default", nil)
	j2 := s.RecordEnqueue("b", nil, "default", nil)
	j3 := s.RecordEnqueue("c", nil, "default", nil)

	if j1.ID == j2.ID || j2.ID == j3.ID {
		t.Errorf("expected unique IDs, got %s, %s, %s", j1.ID, j2.ID, j3.ID)
	}
}
