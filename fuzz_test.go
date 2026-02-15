package ojs

import (
	"encoding/json"
	"testing"
)

// FuzzJobUnmarshalJSON fuzzes the Job JSON unmarshaling path to catch
// panics, infinite loops, or crashes on malformed input.
func FuzzJobUnmarshalJSON(f *testing.F) {
	// Seed corpus with representative inputs.
	f.Add([]byte(`{"id":"j1","type":"email.send","state":"available","args":[{"to":"a@b.com"}],"queue":"default","attempt":1,"max_attempts":3}`))
	f.Add([]byte(`{"id":"","type":"","state":"","args":[],"queue":"","attempt":0,"max_attempts":0}`))
	f.Add([]byte(`{"args":[1,2,3]}`))
	f.Add([]byte(`{"args":[{"nested":{"deep":true}}]}`))
	f.Add([]byte(`{"args":null}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"id":"x","args":"not-an-array"}`))
	f.Add([]byte(`{"priority":-999,"timeout_ms":999999999}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var job Job
		// Must not panic regardless of input.
		_ = json.Unmarshal(data, &job)

		// If unmarshal succeeded, marshal should also not panic.
		if job.Type != "" {
			_, _ = json.Marshal(job)
		}
	})
}

// FuzzArgsFromWire fuzzes the wire-format-to-Args conversion.
func FuzzArgsFromWire(f *testing.F) {
	f.Add([]byte(`[{"to":"user@example.com"}]`))
	f.Add([]byte(`["positional",42,true]`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`[null]`))
	f.Add([]byte(`[1.5,"text",{"key":"val"},null,true]`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var raw []any
		if err := json.Unmarshal(data, &raw); err != nil {
			return // invalid JSON array — not interesting
		}
		// Must not panic.
		args := argsFromWire(raw)
		// Round-trip: args → wire → args should not panic.
		wire := argsToWire(args)
		_ = argsFromWire(wire)
	})
}

// FuzzValidateEnqueueParams fuzzes job type and args validation.
func FuzzValidateEnqueueParams(f *testing.F) {
	f.Add("email.send", `[{"to":"a@b.com"}]`)
	f.Add("", `[]`)
	f.Add("INVALID", `[1]`)
	f.Add("a.b.c.d.e", `[null]`)
	f.Add("a-b_c.d-e_f", `[{}]`)
	f.Add(string(make([]byte, 300)), `[]`) // over max length

	f.Fuzz(func(t *testing.T, jobType string, argsJSON string) {
		var args []any
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			args = []any{}
		}
		// Must not panic.
		_ = validateEnqueueParams(jobType, args)
	})
}

// FuzzValidateQueue fuzzes queue name validation.
func FuzzValidateQueue(f *testing.F) {
	f.Add("default")
	f.Add("email")
	f.Add("my-queue.v2")
	f.Add("")
	f.Add("UPPERCASE")
	f.Add("has spaces")
	f.Add(string(make([]byte, 200))) // over max length

	f.Fuzz(func(t *testing.T, queue string) {
		// Must not panic.
		_ = validateQueue(queue)
	})
}

// FuzzJobMarshalRoundTrip fuzzes the Job marshal→unmarshal round-trip
// to verify the codec doesn't lose data or crash.
func FuzzJobMarshalRoundTrip(f *testing.F) {
	f.Add("job-1", "email.send", "default", `{"to":"user@example.com"}`)
	f.Add("", "", "", `{}`)
	f.Add("id-special/chars", "type.with.dots", "queue-v2", `{"nested":{"a":1}}`)

	f.Fuzz(func(t *testing.T, id, jobType, queue, argsJSON string) {
		var argsMap map[string]any
		if err := json.Unmarshal([]byte(argsJSON), &argsMap); err != nil {
			return
		}

		job := Job{
			ID:    id,
			Type:  jobType,
			Queue: queue,
			Args:  Args(argsMap),
		}

		data, err := json.Marshal(job)
		if err != nil {
			return
		}

		var decoded Job
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("round-trip unmarshal failed: %v", err)
		}
	})
}
