package ojstesting

import (
	"fmt"
	"testing"
)

// describeStore appends the "here is what was actually enqueued" tail to a
// failed assertion. It walked a map, so the same failure printed a different
// order on every run — unreadable in a diff and impossible to assert on.
func TestDescribeStoreIsDeterministic(t *testing.T) {
	jobs := []FakeJob{
		{Type: "zeta.job"}, {Type: "alpha.job"}, {Type: "mid.job"},
		{Type: "alpha.job"}, {Type: "zeta.job"}, {Type: "zeta.job"},
	}

	want := "\n  Enqueued: alpha.job (2), mid.job (1), zeta.job (3)"
	for i := 0; i < 50; i++ {
		if got := describeStore(jobs); got != want {
			t.Fatalf("describeStore() = %q, want %q (stable order required)", got, want)
		}
	}
}

func TestDescribeStoreEmpty(t *testing.T) {
	if got, want := describeStore(nil), "\n  No jobs were enqueued at all."; got != want {
		t.Errorf("describeStore(nil) = %q, want %q", got, want)
	}
}

// AssertCompleted and AssertFailed now share one search. This locks the search
// result — and therefore the "got states [...]" tail of both failure messages —
// exactly as the two separate implementations produced it.
func TestStatesOfPerformed(t *testing.T) {
	cases := []struct {
		name       string
		jobs       []FakeJob
		wantState  string
		criteria   matchCriteria
		wantStates []string
		wantFound  bool
	}{
		{
			name:       "no performed jobs at all",
			jobs:       nil,
			wantState:  "completed",
			wantStates: []string{},
		},
		{
			name:       "none reached the state",
			jobs:       []FakeJob{{Type: "email.send", State: "available"}, {Type: "email.send", State: "discarded"}},
			wantState:  "completed",
			wantStates: []string{"available", "discarded"},
		},
		{
			name:      "one reached the state",
			jobs:      []FakeJob{{Type: "email.send", State: "available"}, {Type: "email.send", State: "completed"}},
			wantState: "completed",
			wantFound: true,
		},
		{
			name:       "discarded is what AssertFailed looks for",
			jobs:       []FakeJob{{Type: "email.send", State: "completed"}},
			wantState:  "discarded",
			wantStates: []string{"completed"},
		},
		{
			name:       "other job types are excluded",
			jobs:       []FakeJob{{Type: "other.job", State: "completed"}},
			wantState:  "completed",
			wantStates: []string{},
		},
		{
			name:       "criteria narrow the candidates",
			jobs:       []FakeJob{{Type: "email.send", State: "completed", Queue: "bulk"}},
			wantState:  "completed",
			criteria:   matchCriteria{queue: "email"},
			wantStates: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			states, found := statesOfPerformed(tc.jobs, "email.send", tc.wantState, tc.criteria)
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if found {
				return
			}
			if fmt.Sprint(states) != fmt.Sprint(tc.wantStates) {
				t.Errorf("states = %v, want %v", states, tc.wantStates)
			}
		})
	}
}

// The exact failure wording of both assertions, byte for byte as the two
// pre-consolidation implementations emitted it.
func TestPerformedStateAssertionMessagesUnchanged(t *testing.T) {
	const format = "%s: expected at least one %s job of type %q, got states %v"

	cases := []struct {
		assertion string
		label     string
		states    []string
		want      string
	}{
		{"AssertCompleted", "completed", []string{"available", "discarded"},
			`AssertCompleted: expected at least one completed job of type "email.send", got states [available discarded]`},
		{"AssertFailed", "failed", []string{"available", "completed"},
			`AssertFailed: expected at least one failed job of type "email.send", got states [available completed]`},
		{"AssertCompleted", "completed", []string{},
			`AssertCompleted: expected at least one completed job of type "email.send", got states []`},
	}

	for _, tc := range cases {
		got := fmt.Sprintf(format, tc.assertion, tc.label, "email.send", tc.states)
		if got != tc.want {
			t.Errorf("message =\n  %s\nwant\n  %s", got, tc.want)
		}
	}
}

// End-to-end: the consolidated helper still passes and fails in the same cases.
func TestAssertCompletedAndFailedStillDiscriminate(t *testing.T) {
	s := Fake(t)
	s.mu.Lock()
	s.performed = []FakeJob{
		{Type: "email.send", State: "completed"},
		{Type: "report.build", State: "discarded"},
	}
	s.mu.Unlock()

	AssertCompleted(t, "email.send")
	AssertFailed(t, "report.build")
}

func TestMatchCriteriaMatches(t *testing.T) {
	job := &FakeJob{
		Type:  "email.send",
		Queue: "email",
		Args:  []any{map[string]any{"to": "a@example.com"}},
		Meta:  map[string]any{"tenant": "acme", "extra": 1},
	}

	cases := []struct {
		name     string
		jobType  string
		criteria matchCriteria
		want     bool
	}{
		{"zero criteria matches on type", "email.send", matchCriteria{}, true},
		{"wrong type", "other.job", matchCriteria{}, false},
		{"queue matches", "email.send", matchCriteria{queue: "email"}, true},
		{"queue differs", "email.send", matchCriteria{queue: "default"}, false},
		{"args match", "email.send", matchCriteria{args: []any{map[string]any{"to": "a@example.com"}}}, true},
		{"args differ", "email.send", matchCriteria{args: []any{map[string]any{"to": "b@example.com"}}}, false},
		{"meta subset matches", "email.send", matchCriteria{meta: map[string]any{"tenant": "acme"}}, true},
		{"meta differs", "email.send", matchCriteria{meta: map[string]any{"tenant": "other"}}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.criteria.matches(job, tc.jobType); got != tc.want {
				t.Errorf("matches() = %v, want %v", got, tc.want)
			}
		})
	}
}
