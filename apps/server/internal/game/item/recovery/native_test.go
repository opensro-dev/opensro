/*
===========================================================================

native_test.go - original x86 queue admission and retirement corpus

The corpus executes the server's linked-list algorithms, including node
insertion and erasure. Only allocation, vital getters and the final keeper
credit boundary are intercepted. Every resulting pulse and queue is checked.

===========================================================================
*/

package recovery

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

/*
================
nativeQueueState
================
*/
type nativeQueueState struct {
	Credit [2]int64
	Queue  [2][][2]int64
}

/*
================
TestOriginalMachineQueueCorpus
================
*/
func TestOriginalMachineQueueCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/native-queue.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		BinarySHA256 string
		Cases        []struct {
			Absolute                 bool
			Current, Maximum, Amount [2]int64
			Before                   [2][][2]int64
			Admitted                 nativeQueueState
			Pulses                   []nativeQueueState
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.BinarySHA256 != "bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290" || len(corpus.Cases) != 2528 {
		t.Fatal("unexpected native corpus identity")
	}
	for i, row := range corpus.Cases {
		var q Queue
		for _, p := range row.Before[0] {
			q.hp = append(q.hp, pulse{p[0], uint8(p[1])})
		}
		for _, p := range row.Before[1] {
			q.mp = append(q.mp, pulse{p[0], uint8(p[1])})
		}
		credit := q.Admit(Admission{
			Current: Amount{row.Current[0], row.Current[1]},
			Maximum: Amount{row.Maximum[0], row.Maximum[1]},
			Credit:  Amount{row.Amount[0], row.Amount[1]}, Absolute: row.Absolute,
		})
		assertNativeQueueState(t, i, -1, &q, credit, row.Admitted)
		for tick, expected := range row.Pulses {
			assertNativeQueueState(t, i, tick, &q, q.Tick(), expected)
		}
	}
}

/*
================
assertNativeQueueState
================
*/
func assertNativeQueueState(t *testing.T, row, tick int, q *Queue, credit Amount, expected nativeQueueState) {
	t.Helper()
	actual := nativeQueueState{Credit: [2]int64{credit.HP, credit.MP}}
	for kind, entries := range [][]pulse{q.hp, q.mp} {
		actual.Queue[kind] = make([][2]int64, 0, len(entries))
		for _, p := range entries {
			actual.Queue[kind] = append(actual.Queue[kind], [2]int64{p.amount, int64(p.remaining)})
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("native case %d tick %d: got %+v, want %+v", row, tick, actual, expected)
	}
}
