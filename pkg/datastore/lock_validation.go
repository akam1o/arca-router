package datastore

func validateLockRequest(req *LockRequest) error {
	if req == nil {
		return NewError(ErrCodeValidation, "lock request is nil", nil)
	}
	return ValidateLockTarget(req.Target)
}
