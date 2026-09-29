package feed

import (
	"errors"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// FeedUse records that a production campaign charged material from a feed
// batch, enabling forward trace from a batch to affected production batches.
type FeedUse struct {
	Batch       string
	Certificate uuid.UUID
	Source      string
	Campaign    uuid.UUID
	Revision    uuid.UUID
	Grade       string
	Qty         float64
	At          int64
}

// Registry is the system of record for feed suitability certificates, grade
// windows, reinspection results and feed-to-campaign usage links.
type Registry struct {
	mu           sync.RWMutex
	certificates map[string]Certificate
	specs        map[string]GradeSpec
	uses         []FeedUse
}

func NewRegistry() *Registry {
	r := &Registry{
		certificates: make(map[string]Certificate),
		specs:        make(map[string]GradeSpec),
		uses:         make([]FeedUse, 0, 32),
	}
	for _, spec := range DefaultGradeSpecs() {
		_ = r.RegisterSpec(spec)
	}
	return r
}

// DefaultGradeSpecs seeds the grade windows used when no window is registered
// explicitly. Values are mass fractions; quantities are in charging units.
func DefaultGradeSpecs() []GradeSpec {
	return []GradeSpec{
		{
			Grade:           "baseline",
			MinComponent:    map[string]float64{"ethanol": 0.80},
			MaxComponent:    map[string]float64{},
			MaxWater:        0.12,
			MaxImpurities:   0.05,
			MaxAllowableQty: 100,
		},
		{
			Grade:           "premium",
			MinComponent:    map[string]float64{"ethanol": 0.90},
			MaxComponent:    map[string]float64{},
			MaxWater:        0.07,
			MaxImpurities:   0.02,
			MaxAllowableQty: 60,
		},
	}
}

// RegisterSpec installs or replaces the admissibility window for a grade.
func (r *Registry) RegisterSpec(spec GradeSpec) error {
	spec.Grade = strings.TrimSpace(spec.Grade)
	if spec.Grade == "" {
		return errors.New("grade is required")
	}
	if math.IsNaN(spec.MaxWater) || spec.MaxWater < 0 || spec.MaxWater > 1 {
		return errors.New("grade water limit outside 0..1")
	}
	if math.IsNaN(spec.MaxImpurities) || spec.MaxImpurities < 0 || spec.MaxImpurities > 1 {
		return errors.New("grade impurity limit outside 0..1")
	}
	for name, min := range spec.MinComponent {
		if strings.TrimSpace(name) == "" {
			return errors.New("component name is required")
		}
		if math.IsNaN(min) || min < 0 || min > 1 {
			return errors.New("component minimum outside 0..1")
		}
		if max, ok := spec.MaxComponent[name]; ok && min > max {
			return errors.New("component minimum exceeds maximum")
		}
	}
	for _, max := range spec.MaxComponent {
		if math.IsNaN(max) || max < 0 || max > 1 {
			return errors.New("component maximum outside 0..1")
		}
	}
	if spec.MaxAllowableQty <= 0 {
		return errors.New("grade allowable quantity must be positive")
	}
	r.mu.Lock()
	r.specs[spec.Grade] = spec
	r.mu.Unlock()
	return nil
}

// Issue files a certificate for its feed batch. Re-issuing replaces the prior
// certificate (renewal) and clears stale reinspection state carried on input.
func (r *Registry) Issue(cert Certificate) error {
	if cert.ID == uuid.Nil || cert.Batch == "" {
		return errors.New("certificate identity is incomplete")
	}
	r.mu.Lock()
	r.certificates[cert.Batch] = cert
	r.mu.Unlock()
	return nil
}

// Reinspect records the latest batch re-test result against a sampling batch.
// A rejected result blocks every new switch-in of the feed batch.
func (r *Registry) Reinspect(batch, sampleBatch string, passed bool, reason string, at int64) (Certificate, error) {
	batch = strings.TrimSpace(batch)
	sampleBatch = strings.TrimSpace(sampleBatch)
	if batch == "" {
		return Certificate{}, errors.New("feed batch is required")
	}
	if sampleBatch == "" {
		return Certificate{}, errors.New("sample batch is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cert, ok := r.certificates[batch]
	if !ok {
		return Certificate{}, errors.New("no suitability certificate for batch")
	}
	cert.Reinspection = &Reinspection{SampleBatch: sampleBatch, Passed: passed, Reason: strings.TrimSpace(reason), At: at}
	r.certificates[batch] = cert
	return cert, nil
}

func (r *Registry) Get(batch string) (Certificate, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cert, ok := r.certificates[strings.TrimSpace(batch)]
	return cert, ok
}

func (r *Registry) List() []Certificate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Certificate, 0, len(r.certificates))
	for _, cert := range r.certificates {
		out = append(out, cert)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Batch < out[j].Batch })
	return out
}

// Authorize admits a source/batch for switching in toward a target grade.
func (r *Registry) Authorize(source, batch, grade string, at int64) (Authorization, error) {
	batch = strings.TrimSpace(batch)
	if batch == "" {
		return Authorization{}, errors.New("feed batch is required")
	}
	r.mu.RLock()
	cert, ok := r.certificates[batch]
	spec, specOK := r.specs[strings.TrimSpace(grade)]
	r.mu.RUnlock()
	if !ok {
		return Authorization{}, errors.New("no suitability certificate for batch " + batch)
	}
	if cert.Source != source {
		return Authorization{}, errors.New("certificate source does not match feed source")
	}
	if !specOK {
		return Authorization{}, errors.New("no grade window registered for " + grade)
	}
	return cert.Evaluate(spec, at), nil
}

// RecordUsage links a charged feed batch to the production campaign using it.
func (r *Registry) RecordUsage(use FeedUse) {
	r.mu.Lock()
	r.uses = append(r.uses, use)
	r.mu.Unlock()
}

// Trace returns every production campaign that charged the feed batch.
func (r *Registry) Trace(batch string) []FeedUse {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]FeedUse, 0)
	for _, use := range r.uses {
		if use.Batch == strings.TrimSpace(batch) {
			out = append(out, use)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}
