package applecontainer

import (
	"reflect"
	"testing"
)

func TestStatsArgsUsesSingleSnapshotJSON(t *testing.T) {
	got := statsArgs("ctr-1", "ctr-2")
	want := []string{"stats", "--format", "json", "--no-stream", "ctr-1", "ctr-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statsArgs() = %v, want %v", got, want)
	}
}
