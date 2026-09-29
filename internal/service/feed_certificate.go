package service

import (
	"time"

	"github.com/wyw14/solo-039/internal/feed"
)

// IssueFeedCertificate files a feed batch suitability certificate carrying
// the measured composition, water and impurity values and the sampling batch.
func (p *Plant) IssueFeedCertificate(source, batch, grade string, assay feed.Assay, validUntil int64) (feed.Certificate, error) {
	cert, err := feed.NewCertificate(source, batch, grade, assay, validUntil)
	if err != nil {
		return feed.Certificate{}, err
	}
	if err := p.FeedRegistry.Issue(cert); err != nil {
		return feed.Certificate{}, err
	}
	_ = p.record("feed.certificate.issued", batch+":"+grade)
	return cert, nil
}

// ReinspectFeedBatch records a batch re-test. A failed reinspection blocks
// every new switch-in of the batch.
func (p *Plant) ReinspectFeedBatch(batch, sampleBatch string, passed bool, reason string) (feed.Certificate, error) {
	cert, err := p.FeedRegistry.Reinspect(batch, sampleBatch, passed, reason, time.Now().Unix())
	if err != nil {
		return feed.Certificate{}, err
	}
	verdict := "passed"
	if !passed {
		verdict = "rejected"
		p.fail(feed.RejectedError{Authorization: feed.Authorization{Batch: batch, Reason: "batch reinspection rejected"}})
	}
	_ = p.record("feed.batch.reinspected", batch+":"+verdict)
	return cert, nil
}

// RegisterGradeSpec installs the admissibility window for a target grade.
func (p *Plant) RegisterGradeSpec(spec feed.GradeSpec) error {
	if err := p.FeedRegistry.RegisterSpec(spec); err != nil {
		return err
	}
	return p.record("feed.grade_spec.registered", spec.Grade)
}

// FeedCertificates lists all filed suitability certificates.
func (p *Plant) FeedCertificates() []feed.Certificate { return p.FeedRegistry.List() }

// TraceFeedBatch returns every production campaign that charged the batch.
func (p *Plant) TraceFeedBatch(batch string) []feed.FeedUse { return p.FeedRegistry.Trace(batch) }

// CurrentFeedAuthorization describes the admission backing the active route.
func (p *Plant) CurrentFeedAuthorization() map[string]any {
	cert, ok := p.FeedRegistry.Get(p.Feed.Current.Batch)
	if !ok {
		return map[string]any{"source": p.Feed.Current.Source, "batch": p.Feed.Current.Batch, "certificate_present": false}
	}
	return map[string]any{
		"source":              cert.Source,
		"batch":               cert.Batch,
		"certificate":         cert.ID,
		"certificate_present": true,
		"target_grade":        cert.TargetGrade,
		"sample_batch":        cert.Assay.SampleBatch,
		"water":               cert.Assay.Water,
		"impurities":          cert.Assay.Impurities,
		"components":          cert.Assay.Components,
		"allowed_qty":         p.Feed.Current.AllowedQty,
		"valid_until":         cert.ValidUntil,
		"expired":             cert.Expired(time.Now().Unix()),
	}
}
