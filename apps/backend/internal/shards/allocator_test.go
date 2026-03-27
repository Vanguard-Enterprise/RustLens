package shards

import (
	"reflect"
	"testing"
)

func TestAssignShardsEvenDistribution(t *testing.T) {
	workers := []string{"bot-b", "bot-a"}
	assignments := AssignShards(6, workers)

	if !reflect.DeepEqual(assignments["bot-a"], []int{0, 2, 4}) {
		t.Fatalf("unexpected bot-a shards: %#v", assignments["bot-a"])
	}
	if !reflect.DeepEqual(assignments["bot-b"], []int{1, 3, 5}) {
		t.Fatalf("unexpected bot-b shards: %#v", assignments["bot-b"])
	}
}

func TestAssignShardsRebalanceAfterWorkerRemoval(t *testing.T) {
	before := AssignShards(4, []string{"bot-a", "bot-b"})
	after := AssignShards(4, []string{"bot-a"})

	if len(before["bot-b"]) == 0 {
		t.Fatalf("expected bot-b to own shards before removal")
	}
	if !reflect.DeepEqual(after["bot-a"], []int{0, 1, 2, 3}) {
		t.Fatalf("unexpected bot-a shards after rebalance: %#v", after["bot-a"])
	}
}
