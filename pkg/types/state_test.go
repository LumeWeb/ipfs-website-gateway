package types

import (
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

func TestStateFromStatus(t *testing.T) {
	cases := []struct {
		status string
		want   SiteState
	}{
		{"active", StateActive},
		{"pending_validation", StatePending},
		{"broken", StateBroken},
		{"", StateUnknown},
		{"some_other_state", StateInactive},
	}

	for _, tc := range cases {
		if got := StateFromStatus(tc.status); got != tc.want {
			t.Errorf("StateFromStatus(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestClassify(t *testing.T) {
	if got := Classify(nil); got != StateUnknown {
		t.Errorf("Classify(nil) = %q, want %q", got, StateUnknown)
	}

	if got := Classify(&GatewayWebsiteResponse{Status: StatusBroken}); got != StateBroken {
		t.Errorf("Classify(broken) = %q, want %q", got, StateBroken)
	}
}

func TestClassifyErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want SiteState
	}{
		{"gone -> broken", ipfs.ErrGone, StateBroken},
		{"not found -> inactive", ipfs.ErrNotFound, StateInactive},
		{"other -> unknown", ipfs.ErrUnauthorized, StateUnknown},
	}

	for _, tc := range cases {
		if got := ClassifyErr(tc.err); got != tc.want {
			t.Errorf("%s: ClassifyErr = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNeedsShortTTL(t *testing.T) {
	if !StatePending.NeedsShortTTL() {
		t.Error("pending should need short TTL")
	}
	if !StateBroken.NeedsShortTTL() {
		t.Error("broken should need short TTL")
	}
	if StateActive.NeedsShortTTL() {
		t.Error("active should not need short TTL")
	}
	if StateInactive.NeedsShortTTL() {
		t.Error("inactive should not need short TTL")
	}
}
