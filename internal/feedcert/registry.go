package feedcert

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	errBatchNotFound   = errors.New("no feed certificate for the batch")
	errSpecMissing     = errors.New("grade spec is not registered")
	errCertificateGone = errors.New("certificate not found")
)

// Registry keeps the grade windows and every issued feed-batch certificate.
type Registry struct {
	mu     sync.RWMutex
	specs  map[string]GradeSpec
	certs  map[uuid.UUID]Certificate
	latest map[string]uuid.UUID // source+batch -> newest certificate
}

func NewRegistry() *Registry {
	return &Registry{specs: make(map[string]GradeSpec), certs: make(map[uuid.UUID]Certificate), latest: make(map[string]uuid.UUID)}
}

// RegisterSpec installs or replaces the acceptance window of a grade.
func (r *Registry) RegisterSpec(spec GradeSpec) error {
	if err := spec.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	r.specs[spec.Grade] = spec
	r.mu.Unlock()
	return nil
}

// Spec returns the registered acceptance window of a grade.
func (r *Registry) Spec(grade string) (GradeSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.specs[grade]
	return spec, ok
}

// Issue records measurements of the sampled lot and stores the certificate.
func (r *Registry) Issue(source, batch, sampleBatch, grade string, components []Measurement, water, impurity float64, issuedAt time.Time) (Certificate, error) {
	r.mu.RLock()
	spec, ok := r.specs[grade]
	r.mu.RUnlock()
	if !ok {
		return Certificate{}, errSpecMissing
	}
	cert, err := NewCertificate(source, batch, sampleBatch, grade, components, water, impurity, issuedAt, spec)
	if err != nil {
		return Certificate{}, err
	}
	r.mu.Lock()
	r.certs[cert.ID] = cert
	r.latest[batchKey(source, batch)] = cert.ID
	r.mu.Unlock()
	return cert, nil
}

// Get returns the certificate by its id.
func (r *Registry) Get(id uuid.UUID) (Certificate, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cert, ok := r.certs[id]
	return cert, ok
}

// Latest returns the newest certificate issued for the given feed batch.
func (r *Registry) Latest(source, batch string) (Certificate, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.latest[batchKey(source, batch)]
	if !ok {
		return Certificate{}, false
	}
	return r.certs[id], true
}

// FailRecheck marks the newest certificate of the feed batch as having failed
// re-inspection; the batch can no longer be cut in until a new certificate is
// issued for a fresh sampled lot.
func (r *Registry) FailRecheck(source, batch string, at time.Time, reason string) (Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.latest[batchKey(source, batch)]
	if !ok {
		return Certificate{}, errBatchNotFound
	}
	cert := r.certs[id]
	cert.RecheckFailed = true
	cert.RecheckAt = at.Unix()
	cert.RecheckReason = reason
	r.certs[id] = cert
	return cert, nil
}

// Judge evaluates the newest certificate of the feed batch against the target
// grade window.
func (r *Registry) Judge(source, batch, grade string, at time.Time) (Decision, Certificate, error) {
	r.mu.RLock()
	id, certOK := r.latest[batchKey(source, batch)]
	cert := r.certs[id]
	spec, specOK := r.specs[grade]
	r.mu.RUnlock()
	if !certOK {
		return Decision{Batch: batch, Grade: grade, Reasons: []string{errBatchNotFound.Error()}}, Certificate{}, errBatchNotFound
	}
	if !specOK {
		return Decision{Batch: batch, Grade: grade, Reasons: []string{errSpecMissing.Error()}}, cert, errSpecMissing
	}
	return Evaluate(cert, spec, at), cert, nil
}

// List returns all certificates, oldest issuance first.
func (r *Registry) List() []Certificate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Certificate, 0, len(r.certs))
	for _, cert := range r.certs {
		out = append(out, cert)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].IssuedAt < out[j].IssuedAt })
	return out
}

func batchKey(source, batch string) string { return source + "\x00" + batch }
