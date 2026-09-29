package feedcert

import (
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
)

// Measurement is one laboratory result taken from the sampled lot.
type Measurement struct {
	Component string
	Value     float64
}

// Certificate is the feed-batch fitness certificate: measured composition,
// water and impurity of the sampled lot, judged against the target grade.
type Certificate struct {
	ID          uuid.UUID
	Source      string
	Batch       string
	SampleBatch string
	Grade       string

	Components []Measurement
	Water      float64
	Impurity   float64

	IssuedAt  int64
	ExpiresAt int64

	RecheckFailed bool
	RecheckAt     int64
	RecheckReason string
}

var (
	errCertificateIdentity = errors.New("certificate source and batch are required")
	errSampleBatch         = errors.New("sampling batch is required")
	errMeasurementEmpty    = errors.New("at least one component measurement is required")
	errMeasurementValue    = errors.New("measurement values must be finite and non-negative")
	errCertificateWindow   = errors.New("certificate validity window is invalid")
	errWaterImpurity       = errors.New("water and impurity must be finite and non-negative")
)

// NewCertificate validates the recorded measurements and stamps the validity
// window from the grade spec. It never evaluates the values against the spec:
// a lot may be certificated as measured and still be outside a grade's window.
func NewCertificate(source, batch, sampleBatch, grade string, components []Measurement, water, impurity float64, issuedAt time.Time, spec GradeSpec) (Certificate, error) {
	if source == "" || batch == "" {
		return Certificate{}, errCertificateIdentity
	}
	if sampleBatch == "" {
		return Certificate{}, errSampleBatch
	}
	if len(components) == 0 {
		return Certificate{}, errMeasurementEmpty
	}
	for _, measurement := range components {
		if measurement.Component == "" {
			return Certificate{}, errMeasurementEmpty
		}
		if !finite(measurement.Value) || measurement.Value < 0 {
			return Certificate{}, errMeasurementValue
		}
	}
	if !finite(water) || !finite(impurity) || water < 0 || impurity < 0 {
		return Certificate{}, errWaterImpurity
	}
	if err := spec.validate(); err != nil {
		return Certificate{}, err
	}
	issued := issuedAt.Unix()
	ttl := spec.CertTTLHours * int64(time.Hour/time.Second)
	return Certificate{
		ID:          uuid.New(),
		Source:      source,
		Batch:       batch,
		SampleBatch: sampleBatch,
		Grade:       grade,
		Components:  append([]Measurement(nil), components...),
		Water:       water,
		Impurity:    impurity,
		IssuedAt:    issued,
		ExpiresAt:   issued + ttl,
	}, nil
}

// Expired reports whether the certificate is past its validity window.
func (c Certificate) Expired(at time.Time) bool { return at.Unix() >= c.ExpiresAt }

// Rejected reports whether a re-inspection of the sampled lot failed.
func (c Certificate) Rejected() bool { return c.RecheckFailed }

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
