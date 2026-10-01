package recruitment

import "testing"

func TestCandidateStageTransitions(t *testing.T) {
	cases := []struct {
		from, to string
		allowed  bool
	}{
		{"inbox", "screening", true}, {"screening", "inbox", true},
		{"inbox", "offer", false}, {"offer", "hired", true},
		{"hired", "rejected", false}, {"interview", "rejected", true},
	}
	for _, test := range cases {
		if actual := CanTransition(test.from, test.to); actual != test.allowed {
			t.Errorf("CanTransition(%q,%q)=%t, want %t", test.from, test.to, actual, test.allowed)
		}
	}
}
