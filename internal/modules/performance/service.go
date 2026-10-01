package performance

import "errors"

var (
	ErrInvalidProgress = errors.New("progress must be between 0 and 100")
	ErrInvalidWeight   = errors.New("weight must be between 0 and 100")
	ErrWeightExceeded  = errors.New("total goal weight cannot exceed 100")
)

func ValidateGoal(progress, weight, currentWeight int) error {
	if progress < 0 || progress > 100 {
		return ErrInvalidProgress
	}
	if weight < 0 || weight > 100 {
		return ErrInvalidWeight
	}
	if currentWeight+weight > 100 {
		return ErrWeightExceeded
	}
	return nil
}
