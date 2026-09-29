package service

import (
	"context"
	"testing"
	"time"

	"github.com/wyw14/solo-039/internal/feed"
)

func newTestPlant(t *testing.T) *Plant {
	t.Helper()
	return NewPlant(t.TempDir())
}

func validAssay(grade string) feed.Assay {
	min := 0.85
	water := 0.10
	impurities := 0.03
	if grade == "premium" {
		min = 0.93
		water = 0.05
		impurities = 0.01
	}
	return feed.Assay{
		Components:  map[string]float64{"ethanol": min},
		Water:       water,
		Impurities:  impurities,
		SampleBatch: "S-" + grade,
		SampledAt:   time.Now().Unix() - 60,
	}
}

func TestStartRequiresAdmittedCertificate(t *testing.T) {
	plant := newTestPlant(t)

	// No certificate at all: switch-in refused.
	if _, err := plant.Start(context.Background(), "baseline", "primary", "B1"); err == nil {
		t.Fatal("start without suitability certificate must be refused")
	}

	cert, err := plant.IssueFeedCertificate("primary", "B1", "baseline", validAssay("baseline"), time.Now().Unix()+3600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Start(context.Background(), "baseline", "primary", "B1"); err != nil {
		t.Fatalf("admitted batch should start: %v", err)
	}
	if plant.Feed.Current.Batch != "B1" || plant.Feed.Current.Certificate != cert.ID {
		t.Fatal("active route must carry batch and certificate")
	}
	uses := plant.TraceFeedBatch("B1")
	if len(uses) != 1 || uses[0].Campaign != plant.Campaign.Snapshot().ID {
		t.Fatal("charged batch must trace to the production campaign")
	}
}

func TestExpiredCertificateBlocksSwitchIn(t *testing.T) {
	plant := newTestPlant(t)
	_, err := plant.IssueFeedCertificate("primary", "OLD", "baseline", validAssay("baseline"), time.Now().Unix()-10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Start(context.Background(), "baseline", "primary", "OLD"); err == nil {
		t.Fatal("expired certificate must block start")
	}
}

func TestFailedReinspectionBlocksNewSwitchAndTracesUsedCampaigns(t *testing.T) {
	plant := newTestPlant(t)
	if _, err := plant.IssueFeedCertificate("primary", "B1", "baseline", validAssay("baseline"), time.Now().Unix()+3600); err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Start(context.Background(), "baseline", "primary", "B1"); err != nil {
		t.Fatal(err)
	}
	firstCampaign := plant.Campaign.Snapshot().ID

	// Batch re-test fails: any new cut-over to it must be blocked.
	if _, err := plant.ReinspectFeedBatch("B1", "S-R2", false, "sediment detected"); err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Transition(context.Background(), "baseline", "primary", "B1"); err == nil {
		t.Fatal("reinspection-failed batch must block new switch-in")
	}
	// The running feed is untouched by the refused cut-over.
	if plant.Feed.Current.Batch != "B1" || !plant.Feed.Current.Open {
		t.Fatal("refused switch must not disturb the running feed route")
	}

	// A fresh batch with a valid certificate can still be cut in.
	if _, err := plant.IssueFeedCertificate("primary", "B2", "baseline", validAssay("baseline"), time.Now().Unix()+3600); err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Transition(context.Background(), "baseline", "primary", "B2"); err != nil {
		t.Fatalf("fresh admitted batch should cut in: %v", err)
	}

	// Tracing the failed batch reaches the production campaign that used it.
	uses := plant.TraceFeedBatch("B1")
	found := false
	for _, use := range uses {
		if use.Campaign == firstCampaign {
			found = true
		}
	}
	if !found {
		t.Fatal("failed batch must trace back to the campaign that already consumed it")
	}
}

func TestOutOfGradeBatchRejected(t *testing.T) {
	plant := newTestPlant(t)
	// Premium needs ethanol >= 0.90; assay at 0.85 is out of range.
	assay := validAssay("baseline")
	if _, err := plant.IssueFeedCertificate("primary", "LOW", "premium", assay, time.Now().Unix()+3600); err != nil {
		t.Fatal(err)
	}
	_, err := plant.Start(context.Background(), "premium", "primary", "LOW")
	if err == nil {
		t.Fatal("batch outside target grade range must be rejected")
	}
}
