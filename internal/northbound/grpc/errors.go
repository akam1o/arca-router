package grpc

import (
	"errors"
	"fmt"
)

var (
	ErrConfigInput              = errors.New("configuration input error")
	ErrCandidateConflict        = errors.New("candidate conflict")
	ErrCommitHistoryUnavailable = errors.New("commit history unavailable")
	ErrSessionNotFound          = errors.New("session not found")
)

type classifiedError struct {
	kind  error
	cause error
	msg   string
}

func (e classifiedError) Error() string {
	return e.msg
}

func (e classifiedError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.cause}
}

func newConfigInputErrorf(format string, args ...any) error {
	return classifiedError{
		kind: ErrConfigInput,
		msg:  fmt.Sprintf(format, args...),
	}
}

func wrapConfigInputErrorf(cause error, format string, args ...any) error {
	return classifiedError{
		kind:  ErrConfigInput,
		cause: cause,
		msg:   fmt.Sprintf(format+": %v", append(args, cause)...),
	}
}

func newCandidateConflictErrorf(format string, args ...any) error {
	return classifiedError{
		kind: ErrCandidateConflict,
		msg:  fmt.Sprintf(format, args...),
	}
}

func newSessionNotFoundErrorf(format string, args ...any) error {
	return classifiedError{
		kind: ErrSessionNotFound,
		msg:  fmt.Sprintf(format, args...),
	}
}
