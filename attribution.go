package payloop

import "errors"

// Attribution tracks cost hierarchy for an invocation.
// ParentID is required. All other fields are optional.
type Attribution struct {
	ParentID        string
	ParentName      *string
	SubsidiaryID    *string
	SubsidiaryName  *string
}

// validate checks if the attribution is valid.
func (a Attribution) validate() error {
	if a.ParentID == "" {
		return errors.New("payloop: parent ID required")
	}
	if len(a.ParentID) > 100 {
		return errors.New("payloop: parent ID exceeds 100 characters")
	}
	if a.ParentName != nil && len(*a.ParentName) > 100 {
		return errors.New("payloop: parent name exceeds 100 characters")
	}
	if a.SubsidiaryID != nil {
		if len(*a.SubsidiaryID) > 100 {
			return errors.New("payloop: subsidiary ID exceeds 100 characters")
		}
	}
	if a.SubsidiaryName != nil && len(*a.SubsidiaryName) > 100 {
		return errors.New("payloop: subsidiary name exceeds 100 characters")
	}
	return nil
}
