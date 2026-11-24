package lwplugin

import (
	"encoding/json"
	"testing"
)

func TestResultUnmarshalOutboxIDVariants(t *testing.T) {
	t.Run("int64", func(t *testing.T) {
		var r Result
		if err := json.Unmarshal([]byte(`{"status":"succeeded","outbox_id":9223372036854775807}`), &r); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.OutboxID == nil || *r.OutboxID != 9223372036854775807 || r.OutboxIDText != "9223372036854775807" {
			t.Fatalf("unexpected outbox_id: %v / %q", r.OutboxID, r.OutboxIDText)
		}
	})

	t.Run("numeric string", func(t *testing.T) {
		var r Result
		if err := json.Unmarshal([]byte(`{"status":"succeeded","outbox_id":"123456789"}`), &r); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.OutboxID == nil || *r.OutboxID != 123456789 || r.OutboxIDText != "123456789" {
			t.Fatalf("unexpected outbox_id: %v / %q", r.OutboxID, r.OutboxIDText)
		}
	})

	t.Run("non-numeric string", func(t *testing.T) {
		var r Result
		if err := json.Unmarshal([]byte(`{"status":"succeeded","outbox_id":"outbox_abc-123"}`), &r); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.OutboxID != nil || r.OutboxIDText != "outbox_abc-123" {
			t.Fatalf("unexpected outbox_id: %v / %q", r.OutboxID, r.OutboxIDText)
		}
	})
}
