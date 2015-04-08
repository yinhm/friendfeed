package store

// unsigned 32-bit error code
type ErrorCode uint32

const (
	OK            ErrorCode = 0
	Unknown       ErrorCode = 1
	StopIteration ErrorCode = 2
	ExistItem     ErrorCode = 3
)
