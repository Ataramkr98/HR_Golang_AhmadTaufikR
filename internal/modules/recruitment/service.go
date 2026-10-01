package recruitment

var stageIndex = map[string]int{"inbox": 0, "screening": 1, "interview": 2, "offer": 3, "hired": 4}

func CanTransition(from, to string) bool {
	if from == to {
		return true
	}
	if to == "rejected" {
		return from != "hired" && from != "rejected"
	}
	if from == "rejected" {
		return to == "inbox"
	}
	fromIndex, fromOK := stageIndex[from]
	toIndex, toOK := stageIndex[to]
	if !fromOK || !toOK {
		return false
	}
	difference := fromIndex - toIndex
	return difference == 1 || difference == -1
}
