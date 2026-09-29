package feedcert

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Violation explains why one measured value keeps the lot out of a grade.
type Violation struct {
	Kind  string // "component" | "water" | "impurity" | "missing-component"
	Name  string
	Value float64
	Min   float64
	Max   float64
}

func (v Violation) Error() string {
	switch v.Kind {
	case "water":
		return fmt.Sprintf("water %.4f exceeds grade limit %.4f", v.Value, v.Max)
	case "impurity":
		return fmt.Sprintf("impurity %.4f exceeds grade limit %.4f", v.Value, v.Max)
	case "missing-component":
		return fmt.Sprintf("component %q was not measured for the grade", v.Name)
	default:
		return fmt.Sprintf("component %q %.4f outside grade range [%.4f, %.4f]", v.Name, v.Value, v.Min, v.Max)
	}
}

// Decision is the fitness verdict of one lot against one target grade.
type Decision struct {
	CertificateID string
	Batch         string
	Grade         string
	Allowed       bool
	UsableAmount  float64
	Violations    []Violation
	Reasons       []string
}

func (d Decision) Error() error {
	if d.Allowed {
		return nil
	}
	if len(d.Reasons) == 0 {
		return errors.New("feed batch is not fit for the target grade")
	}
	return errors.New(d.Reasons[0])
}

// FullUsableAmount means the whole lot may be dosed; any positive value up to
// the lot size is accepted.
const FullUsableAmount = 1.0

// Evaluate judges a certificate against the target grade spec at the given
// moment. Expiry or a failed re-inspection block the lot regardless of the
// measured values. Otherwise each required indicator is checked against the
// grade window and the usable fraction is driven by the tightest headroom:
//
//	usable = 1                       when every value sits at/below its limit
//	usable shrinks toward 0          as the tightest value approaches its limit
//	usable = 0 (blocked)             when any value leaves the window
func Evaluate(cert Certificate, spec GradeSpec, at time.Time) Decision {
	decision := Decision{CertificateID: cert.ID.String(), Batch: cert.Batch, Grade: spec.Grade, UsableAmount: FullUsableAmount}

	// The certificate stores physical measurements plus the grade it was
	// submitted for; the same lot can be re-judged against any target grade.
	switch {
	case cert.RecheckFailed:
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("feed batch %s failed re-inspection%s", cert.Batch, recheckSuffix(cert.RecheckReason)))
		return decision
	case cert.Expired(at):
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("feed certificate %s for batch %s expired at %s", cert.ID, cert.Batch, time.Unix(cert.ExpiresAt, 0).Format(time.RFC3339)))
		return decision
	}

	measured := make(map[string]float64, len(cert.Components))
	for _, m := range cert.Components {
		measured[m.Component] = m.Value
	}

	names := make([]string, 0, len(spec.Components))
	for name := range spec.Components {
		names = append(names, name)
	}
	sort.Strings(names)

	tightest := 1.0
	for _, name := range names {
		r := spec.Components[name]
		value, ok := measured[name]
		if !ok {
			decision.Violations = append(decision.Violations, Violation{Kind: "missing-component", Name: name})
			continue
		}
		if !r.Contains(value) {
			decision.Violations = append(decision.Violations, Violation{Kind: "component", Name: name, Value: value, Min: r.Min, Max: r.Max})
			continue
		}
		if headroom := r.Headroom(value); headroom < tightest {
			tightest = headroom
		}
	}
	if cert.Water > spec.MaxWater {
		decision.Violations = append(decision.Violations, Violation{Kind: "water", Value: cert.Water, Max: spec.MaxWater})
	} else if headroom := upperHeadroom(cert.Water, spec.MaxWater); headroom < tightest {
		tightest = headroom
	}
	if cert.Impurity > spec.MaxImpurity {
		decision.Violations = append(decision.Violations, Violation{Kind: "impurity", Value: cert.Impurity, Max: spec.MaxImpurity})
	} else if headroom := upperHeadroom(cert.Impurity, spec.MaxImpurity); headroom < tightest {
		tightest = headroom
	}

	for _, violation := range decision.Violations {
		decision.Reasons = append(decision.Reasons, violation.Error())
	}
	if len(decision.Violations) > 0 {
		decision.UsableAmount = 0
		return decision
	}
	decision.Allowed = true
	decision.UsableAmount = tightest
	return decision
}

func upperHeadroom(value, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	headroom := (limit - value) / limit
	if headroom < 0 {
		return 0
	}
	if headroom > 1 {
		return 1
	}
	return headroom
}

func recheckSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}
