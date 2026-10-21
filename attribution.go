package crystil

import (
	"fmt"
	"unicode/utf8"
)

// Attribution tracks cost hierarchy for an invocation.
// ParentID is required. All other fields are optional.
type Attribution struct {
	ParentID       string
	ParentName     *string
	SubsidiaryID   *string
	SubsidiaryName *string
}

// validate checks if the attribution is valid.
func (a Attribution) validate() error {
	if a.ParentID == "" {
		return fmt.Errorf("%w: parent ID required", ErrInvalidAttribution)
	}
	if tooLong(a.ParentID) {
		return fmt.Errorf("%w: parent ID exceeds 100 characters", ErrInvalidAttribution)
	}
	if a.ParentName != nil && tooLong(*a.ParentName) {
		return fmt.Errorf("%w: parent name exceeds 100 characters", ErrInvalidAttribution)
	}
	if a.SubsidiaryID != nil && tooLong(*a.SubsidiaryID) {
		return fmt.Errorf("%w: subsidiary ID exceeds 100 characters", ErrInvalidAttribution)
	}
	if a.SubsidiaryName != nil && a.SubsidiaryID == nil {
		return fmt.Errorf("%w: subsidiary ID required with a subsidiary name", ErrInvalidAttribution)
	}
	if a.SubsidiaryName != nil && tooLong(*a.SubsidiaryName) {
		return fmt.Errorf("%w: subsidiary name exceeds 100 characters", ErrInvalidAttribution)
	}
	return nil
}

// tooLong reports whether s is over 100 characters, counted as Python counts
// them, not in bytes.
func tooLong(s string) bool {
	return utf8.RuneCountInString(s) > 100
}

// payload returns the attribution as the payload records it. It copies every
// value, so later changes to a cannot reach it.
func (a Attribution) payload() map[string]interface{} {
	parent := map[string]interface{}{"id": a.ParentID}
	if a.ParentName != nil {
		parent["name"] = *a.ParentName
	}
	payload := map[string]interface{}{"parent": parent}
	if a.SubsidiaryID != nil {
		subsidiary := map[string]interface{}{"id": *a.SubsidiaryID}
		if a.SubsidiaryName != nil {
			subsidiary["name"] = *a.SubsidiaryName
		}
		payload["subsidiary"] = subsidiary
	}
	return payload
}
