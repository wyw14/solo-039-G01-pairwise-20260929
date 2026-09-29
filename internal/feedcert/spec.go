package feedcert

import "errors"

// Range is the inclusive acceptance window of one measured value for a grade.
type Range struct {
	Min float64
	Max float64
}

func (r Range) Contains(value float64) bool { return value >= r.Min && value <= r.Max }

// Headroom returns the fraction of the upper limit still available, 1 at the
// lower edge and 0 at the upper edge.
func (r Range) Headroom(value float64) float64 {
	if r.Max <= r.Min {
		return 0
	}
	headroom := (r.Max - value) / (r.Max - r.Min)
	if headroom < 0 {
		return 0
	}
	if headroom > 1 {
		return 1
	}
	return headroom
}

// GradeSpec is the feed composition window a target grade requires.
type GradeSpec struct {
	Grade        string
	Components   map[string]Range
	MaxWater     float64
	MaxImpurity  float64
	CertTTLHours int64
}

var (
	errComponentRange = errors.New("component range is invalid")
	errLimitRange     = errors.New("water/impurity limit must be positive")
)

func (s GradeSpec) validate() error {
	if s.Grade == "" {
		return errors.New("grade is required")
	}
	for name, value := range s.Components {
		if name == "" {
			return errors.New("component name is required")
		}
		if value.Min > value.Max || value.Min < 0 {
			return errComponentRange
		}
	}
	if s.MaxWater <= 0 || s.MaxImpurity <= 0 {
		return errLimitRange
	}
	if s.CertTTLHours <= 0 {
		return errors.New("certificate ttl is required")
	}
	return nil
}

func (s GradeSpec) component(name string) (Range, bool) {
	value, ok := s.Components[name]
	return value, ok
}
