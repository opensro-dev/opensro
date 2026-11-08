/*
===========================================================================

silkhistory_test.go - silk charge history control admission and projection

===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestMallSilkHistoryAnswer

A valid inquiry answers with the session account's uses for the chosen
period, newest first, with whole-second timestamps and the period total.
================
*/
func TestMallSilkHistoryAnswer(t *testing.T) {
	rt, c, authority, _ := mallFixture()
	authority.history = []domain.SilkHistoryRow{
		{AtMs: 1_700_000_100_000, Charged: 32},
		{AtMs: 1_699_999_000_500, Charged: 5},
	}
	authority.historyTotal = 37
	result := rt.HandleMallSilkHistory(testDivision, c, []byte(`{"period":1}`))
	if len(result.Frames) != 1 || result.Frames[0].Opcode != opMallSilkHistory {
		t.Fatalf("inquiry frames: %+v", result)
	}
	var answer mallSilkHistoryProjection
	if err := json.Unmarshal(result.Frames[0].Payload, &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Version != 1 || answer.Period != 1 || answer.Total != 37 || len(answer.Rows) != 2 ||
		answer.Rows[0] != (mallSilkHistoryEntry{At: 1_700_000_100, Charged: 32}) ||
		answer.Rows[1] != (mallSilkHistoryEntry{At: 1_699_999_000, Charged: 5}) {
		t.Fatalf("history answer: %+v", answer)
	}
	for period := 0; period < len(mallSilkPeriodDays); period++ {
		result := rt.HandleMallSilkHistory(testDivision, c, []byte(fmt.Sprintf(`{"period":%d}`, period)))
		if len(result.Frames) != 1 || result.Frames[0].Opcode != opMallSilkHistory {
			t.Fatalf("period %d frames: %+v", period, result)
		}
	}
}

/*
================
TestMallSilkHistoryRefusals

Forged inquiries - malformed JSON, unknown fields, out-of-range periods,
empty payloads, a missing authority - never reach the store.
================
*/
func TestMallSilkHistoryRefusals(t *testing.T) {
	for _, kind := range []string{"malformed", "unknown-field", "period-high", "period-negative", "empty", "unbound", "no-authority"} {
		t.Run(kind, func(t *testing.T) {
			rt, c, authority, _ := mallFixture()
			authority.history = []domain.SilkHistoryRow{{AtMs: 1_700_000_100_000, Charged: 32}}
			var payload []byte
			switch kind {
			case "malformed":
				payload = []byte(`{"period":`)
			case "unknown-field":
				payload = []byte(`{"period":1,"account":"victim"}`)
			case "period-high":
				payload = []byte(`{"period":4}`)
			case "period-negative":
				payload = []byte(`{"period":-1}`)
			case "unbound":
				c = nil
				payload = []byte(`{"period":1}`)
			case "no-authority":
				rt.mallAuthority = nil
				payload = []byte(`{"period":1}`)
			}
			result := rt.HandleMallSilkHistory(testDivision, c, payload)
			if len(result.Frames) == 1 && result.Frames[0].Opcode == opMallSilkHistory {
				t.Fatalf("forged inquiry answered: %+v", result)
			}
		})
	}
}
