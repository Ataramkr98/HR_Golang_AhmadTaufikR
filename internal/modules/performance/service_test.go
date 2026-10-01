package performance

import "testing"

func TestValidateGoalRejectsWeightOverflow(t *testing.T) {
	if err := ValidateGoal(50, 30, 80); err != ErrWeightExceeded {
		t.Fatalf("expected ErrWeightExceeded, got %v", err)
	}
}
