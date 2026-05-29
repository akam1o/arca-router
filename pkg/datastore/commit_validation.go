package datastore

func validateCommitRequest(req *CommitRequest) error {
	if req == nil {
		return NewError(ErrCodeValidation, "commit request is nil", nil)
	}
	return nil
}

func validateRollbackRequest(req *RollbackRequest) error {
	if req == nil {
		return NewError(ErrCodeValidation, "rollback request is nil", nil)
	}
	return nil
}
