package feed

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Assay holds the measured quality values of a feed batch. Values are mass
// fractions in the 0..1 range.
type Assay struct {
	Components  map[string]float64
	Water       float64
	Impurities  float64
	SampleBatch string
	SampledAt   int64
}

// GradeSpec is the target grade window a feed batch is admitted against.
// Component limits are inclusive; absent component keys are not constrained.
type GradeSpec struct {
	Grade           string
	MinComponent    map[string]float64
	MaxComponent    map[string]float64
	MaxWater        float64
	MaxImpurities   float64
	MaxAllowableQty float64
}

// Reinspection is the latest batch re-test result. Rejected tests block the
// batch from being switched in.
type Reinspection struct {
	SampleBatch string
	Passed      bool
	Reason      string
	At          int64
}

// Certificate is a feed batch suitability certificate. It binds measured
// composition to a target grade window and expires once ValidUntil passes.
type Certificate struct {
	ID           uuid.UUID
	Source       string
	Batch        string
	Assay        Assay
	TargetGrade  string
	ValidUntil   int64
	Reinspection *Reinspection
	IssuedAt     int64
}

// Authorization is the result of admitting a certificate for a switch.
type Authorization struct {
	Allowed     bool
	Reason      string
	Batch       string
	Certificate uuid.UUID
	Grade       string
	AllowedQty  float64
	ExpiresAt   int64
	SampleBatch string
	CheckedAt   int64
}

func nowUnix() int64 { return time.Now().Unix() }

func (a Assay) validate() error {
	if strings.TrimSpace(a.SampleBatch) == "" {
		return errors.New("sample batch is required")
	}
	if len(a.Components) == 0 {
		return errors.New("component measurements are required")
	}
	total := 0.0
	for name, value := range a.Components {
		if strings.TrimSpace(name) == "" {
			return errors.New("component name is required")
		}
		if math.IsNaN(value) || value < 0 || value > 1 {
			return errors.New("component measurement outside 0..1")
		}
		total += value
	}
	if total > 1+1e-9 {
		return errors.New("component measurements exceed 1.0 mass fraction")
	}
	if math.IsNaN(a.Water) || a.Water < 0 || a.Water > 1 {
		return errors.New("water measurement outside 0..1")
	}
	if math.IsNaN(a.Impurities) || a.Impurities < 0 || a.Impurities > 1 {
		return errors.New("impurity measurement outside 0..1")
	}
	return nil
}

// NewCertificate issues a certificate after validating the measured assay.
func NewCertificate(source, batch, grade string, assay Assay, validUntil int64) (Certificate, error) {
	source = strings.TrimSpace(source)
	batch = strings.TrimSpace(batch)
	grade = strings.TrimSpace(grade)
	if source == "" {
		return Certificate{}, errors.New("feed source is required")
	}
	if batch == "" {
		return Certificate{}, errors.New("feed batch is required")
	}
	if grade == "" {
		return Certificate{}, errors.New("target grade is required")
	}
	if err := assay.validate(); err != nil {
		return Certificate{}, err
	}
	if validUntil <= assay.SampledAt {
		return Certificate{}, errors.New("certificate must remain valid after the sampling time")
	}
	return Certificate{
		ID:          uuid.New(),
		Source:      source,
		Batch:       batch,
		Assay:       assay,
		TargetGrade: grade,
		ValidUntil:  validUntil,
		IssuedAt:    nowUnix(),
	}, nil
}

// Expired reports whether the certificate is past its validity window.
func (c Certificate) Expired(at int64) bool { return at > c.ValidUntil }

// Evaluate admits the certificate against the grade spec at the given time.
// AllowedQty is the quantity that may be charged: zero whenever the batch is
// not admissible, otherwise the spec allowable quantity.
func (c Certificate) Evaluate(spec GradeSpec, at int64) Authorization {
	auth := Authorization{
		Batch:       c.Batch,
		Certificate: c.ID,
		Grade:       spec.Grade,
		ExpiresAt:   c.ValidUntil,
		SampleBatch: c.Assay.SampleBatch,
		CheckedAt:   at,
	}
	reject := func(reason string) Authorization {
		auth.Allowed = false
		auth.Reason = reason
		auth.AllowedQty = 0
		return auth
	}
	if spec.Grade != c.TargetGrade {
		return reject("certificate is not issued for grade " + spec.Grade)
	}
	if c.Expired(at) {
		return reject("feed suitability certificate expired")
	}
	if c.Reinspection != nil && !c.Reinspection.Passed {
		return reject("batch reinspection rejected: " + c.Reinspection.Reason)
	}
	for name, value := range c.Assay.Components {
		if min, ok := spec.MinComponent[name]; ok && value < min {
			return reject("component " + name + " below grade minimum")
		}
		if max, ok := spec.MaxComponent[name]; ok && value > max {
			return reject("component " + name + " above grade maximum")
		}
	}
	if c.Assay.Water > spec.MaxWater {
		return reject("water content above grade limit")
	}
	if c.Assay.Impurities > spec.MaxImpurities {
		return reject("impurity content above grade limit")
	}
	auth.Allowed = true
	auth.Reason = "within target grade range"
	auth.AllowedQty = spec.MaxAllowableQty
	return auth
}
