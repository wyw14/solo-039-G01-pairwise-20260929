package feedcert

import (
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Usage records one cut-in: a production batch (campaign revision) began to
// consume a feed batch under a fitness certificate.
type Usage struct {
	FeedSource    string
	FeedBatch     string
	CertificateID uuid.UUID
	CampaignID    uuid.UUID
	CampaignRev   uuid.UUID
	Grade         string
	UsableAmount  float64
	At            int64
}

// UsageLedger links feed batches to the production batches that consumed them,
// in both directions.
type UsageLedger struct {
	mu    sync.Mutex
	usage []Usage
}

func NewUsageLedger() *UsageLedger { return &UsageLedger{} }

// Record logs a cut-in. It must only be called after the certificate has been
// judged fit for the target grade.
func (l *UsageLedger) Record(value Usage) {
	value.At = time.Now().Unix()
	l.mu.Lock()
	l.usage = append(l.usage, value)
	l.mu.Unlock()
}

// ForFeedBatch returns every production batch that used the feed batch.
func (l *UsageLedger) ForFeedBatch(source, batch string) []Usage {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Usage, 0)
	for _, value := range l.usage {
		if value.FeedSource == source && value.FeedBatch == batch {
			out = append(out, value)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// ForCampaign returns every feed batch cut into the production batch.
func (l *UsageLedger) ForCampaign(campaignID uuid.UUID) []Usage {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Usage, 0)
	for _, value := range l.usage {
		if value.CampaignID == campaignID {
			out = append(out, value)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// Count returns the number of recorded cut-ins.
func (l *UsageLedger) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.usage)
}
