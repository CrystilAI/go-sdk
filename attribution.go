package payloop

import "errors"

// Attribution tracks cost hierarchy for an invocation.
type Attribution struct {
	Parent     AttributionEntity
	Subsidiary *AttributionEntity // nil if not applicable
}

// AttributionEntity represents a cost attribution node.
type AttributionEntity struct {
	ID   string
	Name string
}

// validate checks if the attribution is valid.
func (a Attribution) validate() error {
	if a.Parent.ID == "" {
		return errors.New("payloop: parent ID required")
	}
	if len(a.Parent.ID) > 100 {
		return errors.New("payloop: parent ID exceeds 100 characters")
	}
	if a.Parent.Name != "" && len(a.Parent.Name) > 100 {
		return errors.New("payloop: parent name exceeds 100 characters")
	}
	if a.Subsidiary != nil {
		if a.Subsidiary.ID == "" {
			return errors.New("payloop: subsidiary ID required when subsidiary is set")
		}
		if len(a.Subsidiary.ID) > 100 {
			return errors.New("payloop: subsidiary ID exceeds 100 characters")
		}
		if a.Subsidiary.Name != "" && len(a.Subsidiary.Name) > 100 {
			return errors.New("payloop: subsidiary name exceeds 100 characters")
		}
	}
	return nil
}
