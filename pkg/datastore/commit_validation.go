package datastore

func validateCommitRequest(req *CommitRequest) error {
	if req == nil {
		return NewError(ErrCodeValidation, "commit request is nil", nil)
	}
	return nil
}
