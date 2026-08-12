package state

import "math"

func unreachableCapacityHint(byteHint, countHintLimit uint64) int {
	const estimatedSerializedUint32Bytes = 6
	hint := byteHint / estimatedSerializedUint32Bytes
	if countHintLimit > 0 && hint > countHintLimit {
		hint = countHintLimit
	}
	maxInt := uint64(math.MaxInt)
	if hint > maxInt {
		hint = maxInt
	}
	return int(hint)
}
