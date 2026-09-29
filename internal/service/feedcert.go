package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/feedcert"
	"github.com/wyw14/solo-039/internal/model"
)

func newFeedRegistry() *feedcert.Registry {
	registry := feedcert.NewRegistry()
	feedcert.RegisterDefaults(registry)
	return registry
}

var (
	errFeedBatchRequired = errors.New("feed batch is required")
	errFeedNotFit        = errors.New("feed batch is not fit for the target grade")
)

// authorizeFeed is the cut-in gate: the newest fitness certificate for the
// feed batch must judge the lot fit for the target grade right now. Expiry, a
// failed re-inspection, a missing certificate or any indicator outside the
// grade window all block the cut-in.
func (p *Plant) authorizeFeed(source, batch, grade string) (feedcert.Decision, feedcert.Certificate, error) {
	if batch == "" {
		return feedcert.Decision{}, feedcert.Certificate{}, errFeedBatchRequired
	}
	now := time.Now()
	decision, cert, err := p.FeedCerts.Judge(source, batch, grade, now)
	if err != nil {
		return decision, cert, fmt.Errorf("feed fitness gate: %w", err)
	}
	if !decision.Allowed {
		return decision, cert, fmt.Errorf("%w: %s", errFeedNotFit, joinReasons(decision.Reasons))
	}
	return decision, cert, nil
}

// recordFeedUsage links the consumed feed batch to the production batch that
// is now running, so a later re-inspection failure can be traced forward.
func (p *Plant) recordFeedUsage(source, batch, grade string, cert feedcert.Certificate, decision feedcert.Decision, current model.Campaign) {
	p.FeedUsage.Record(feedcert.Usage{
		FeedSource:    source,
		FeedBatch:     batch,
		CertificateID: cert.ID,
		CampaignID:    current.ID,
		CampaignRev:   current.Revision,
		Grade:         grade,
		UsableAmount:  decision.UsableAmount,
	})
	_ = p.record("feed.certificate.used", fmt.Sprintf("%s:%s:%s:%.4f", source, batch, cert.ID, decision.UsableAmount))
}

// IssueFeedCertificate records measured composition, water and impurity of
// the sampled lot and issues the fitness certificate for the grade.
func (p *Plant) IssueFeedCertificate(input FeedCertificateInput) (feedcert.Certificate, error) {
	measurements := make([]feedcert.Measurement, 0, len(input.Components))
	for name, value := range input.Components {
		measurements = append(measurements, feedcert.Measurement{Component: name, Value: value})
	}
	cert, err := p.FeedCerts.Issue(input.Source, input.Batch, input.SampleBatch, input.Grade, measurements, input.Water, input.Impurity, time.Now())
	if err != nil {
		return feedcert.Certificate{}, err
	}
	_ = p.record("feed.certificate.issued", fmt.Sprintf("%s:%s:%s:%s", input.Source, input.Batch, input.SampleBatch, cert.ID))
	return cert, nil
}

// FailFeedRecheck records a failed re-inspection of the feed batch. The lot is
// blocked from new cut-ins immediately; the production batches that already
// consumed it are returned for containment.
func (p *Plant) FailFeedRecheck(source, batch, reason string) (FeedRecheckResult, error) {
	cert, err := p.FeedCerts.FailRecheck(source, batch, time.Now(), reason)
	if err != nil {
		return FeedRecheckResult{}, err
	}
	traced := p.FeedUsage.ForFeedBatch(source, batch)
	production := make([]FeedBatchConsumer, 0, len(traced))
	for _, usage := range traced {
		production = append(production, FeedBatchConsumer{
			CampaignID:    usage.CampaignID,
			CampaignRev:   usage.CampaignRev,
			Grade:         usage.Grade,
			CertificateID: usage.CertificateID,
			UsableAmount:  usage.UsableAmount,
			At:            usage.At,
		})
	}
	_ = p.record("feed.recheck.failed", fmt.Sprintf("%s:%s:%s:consumers=%d", source, batch, cert.ID, len(production)))
	p.Incidents.Open("feed-recheck", fmt.Sprintf("feed batch %s (%s) failed re-inspection: %s", batch, source, reason), p.Campaign.Snapshot().Revision.String())
	return FeedRecheckResult{Certificate: cert, Consumers: production}, nil
}

// ListFeedCertificates returns every issued fitness certificate.
func (p *Plant) ListFeedCertificates() []feedcert.Certificate { return p.FeedCerts.List() }

// TraceFeedBatch returns the production batches that consumed a feed batch.
func (p *Plant) TraceFeedBatch(source, batch string) ([]FeedBatchConsumer, error) {
	if _, ok := p.FeedCerts.Latest(source, batch); !ok {
		return nil, errors.New("no feed certificate for the batch")
	}
	traced := p.FeedUsage.ForFeedBatch(source, batch)
	consumers := make([]FeedBatchConsumer, 0, len(traced))
	for _, usage := range traced {
		consumers = append(consumers, FeedBatchConsumer{
			CampaignID:    usage.CampaignID,
			CampaignRev:   usage.CampaignRev,
			Grade:         usage.Grade,
			CertificateID: usage.CertificateID,
			UsableAmount:  usage.UsableAmount,
			At:            usage.At,
		})
	}
	return consumers, nil
}

// CurrentFeedFitness reports the verdict for the feed route currently active.
func (p *Plant) CurrentFeedFitness() map[string]any {
	source := p.Feed.Current.Source
	grade := p.Campaign.Snapshot().Grade
	out := map[string]any{"source": source, "grade": grade, "batch_required": true}
	if source == "" {
		return out
	}
	latest, ok := p.FeedCerts.Latest(source, p.currentBatch(source))
	if !ok {
		out["certified"] = false
		return out
	}
	spec, specOK := p.FeedCerts.Spec(grade)
	if !specOK {
		out["certified"] = true
		out["certificate"] = latest
		return out
	}
	decision := feedcert.Evaluate(latest, spec, time.Now())
	out["certified"] = true
	out["certificate"] = latest
	out["decision"] = decision
	return out
}

// currentBatch returns the batch recorded for the active route, if any.
func (p *Plant) currentBatch(source string) string {
	for _, usage := range p.FeedUsage.ForCampaign(p.Campaign.Snapshot().ID) {
		if usage.FeedSource == source {
			return usage.FeedBatch
		}
	}
	return ""
}

func joinReasons(reasons []string) string {
	joined := ""
	for i, reason := range reasons {
		if i > 0 {
			joined += "; "
		}
		joined += reason
	}
	return joined
}

// FeedCertificateInput is the API payload for issuing a fitness certificate.
type FeedCertificateInput struct {
	Source      string             `json:"source"`
	Batch       string             `json:"batch"`
	SampleBatch string             `json:"sample_batch"`
	Grade       string             `json:"grade"`
	Components  map[string]float64 `json:"components"`
	Water       float64            `json:"water"`
	Impurity    float64            `json:"impurity"`
}

// FeedBatchConsumer identifies one production batch that used a feed lot.
type FeedBatchConsumer struct {
	CampaignID    uuid.UUID `json:"campaign_id"`
	CampaignRev   uuid.UUID `json:"campaign_revision"`
	Grade         string    `json:"grade"`
	CertificateID uuid.UUID `json:"certificate_id"`
	UsableAmount  float64   `json:"usable_amount"`
	At            int64     `json:"at"`
}

// FeedRecheckResult is returned when a re-inspection fails: the blocked
// certificate plus every production batch already exposed to the lot.
type FeedRecheckResult struct {
	Certificate feedcert.Certificate `json:"certificate"`
	Consumers   []FeedBatchConsumer  `json:"consumed_by"`
}
