package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wyw14/solo-039/internal/feedcert"
)

func fitBaselineInput(batch, sampleBatch string) FeedCertificateInput {
	return FeedCertificateInput{
		Source:      "primary",
		Batch:       batch,
		SampleBatch: sampleBatch,
		Grade:       "baseline",
		Components:  map[string]float64{"ethanol": 0.90},
		Water:       0.05,
		Impurity:    0.02,
	}
}

func fitPremiumInput(batch, sampleBatch string) FeedCertificateInput {
	input := fitBaselineInput(batch, sampleBatch)
	input.Grade = "premium"
	input.Components = map[string]float64{"ethanol": 0.94}
	input.Water = 0.02
	input.Impurity = 0.01
	return input
}

func TestFeedCertificateGateOnStart(t *testing.T) {
	plant := NewPlant(t.TempDir())

	// No batch at all: cut-in refused.
	if _, err := plant.Start(context.Background(), "baseline", "primary", ""); err == nil {
		t.Fatal("start without a feed batch must be refused")
	}
	// Batch named but no certificate: refused.
	if _, err := plant.Start(context.Background(), "baseline", "primary", "B1"); err == nil {
		t.Fatal("start without a fitness certificate must be refused")
	}

	if _, err := plant.IssueFeedCertificate(fitBaselineInput("B1", "S1")); err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Start(context.Background(), "baseline", "primary", "B1"); err != nil {
		t.Fatalf("certified fit lot must start: %v", err)
	}
	if plant.FeedUsage.Count() != 1 {
		t.Fatalf("expected one recorded feed usage, got %d", plant.FeedUsage.Count())
	}
}

func TestTransitionBlockedByGradeWindowAndRecheck(t *testing.T) {
	plant := NewPlant(t.TempDir())
	if _, err := plant.IssueFeedCertificate(fitBaselineInput("B1", "S1")); err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Start(context.Background(), "baseline", "primary", "B1"); err != nil {
		t.Fatal(err)
	}

	// The baseline-certified lot (ethanol 0.90) is below the premium window.
	if _, err := plant.Transition(context.Background(), "premium", "primary", "B1"); err == nil {
		t.Fatal("lot outside premium window must block the cut-in")
	} else if !strings.Contains(err.Error(), "not fit") {
		t.Fatalf("expected fitness error, got %v", err)
	}
	// Failed cut-in must not advance the campaign.
	if phase := plant.Campaign.Snapshot().Phase; phase != "stabilizing" {
		t.Fatalf("campaign phase changed after blocked transition: %s", phase)
	}

	// Re-inspection fails: cut-in blocked even against the same grade.
	result, err := plant.FailFeedRecheck("primary", "B1", "methanol climb")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Consumers) != 1 {
		t.Fatalf("the running production batch must be traced as a consumer, got %d", len(result.Consumers))
	}
	if _, err := plant.Transition(context.Background(), "baseline", "primary", "B1"); err == nil {
		t.Fatal("rejected lot must block new cut-in")
	}

	// Fresh sampled lot, new certificate restores fitness for premium.
	if _, err := plant.IssueFeedCertificate(fitPremiumInput("B1", "S2")); err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Transition(context.Background(), "premium", "primary", "B1"); err != nil {
		t.Fatalf("new certificate must clear the block: %v", err)
	}

	consumers, err := plant.TraceFeedBatch("primary", "B1")
	if err != nil {
		t.Fatal(err)
	}
	if len(consumers) != 2 {
		t.Fatalf("expected two production batches traced to B1 (start + transition), got %d", len(consumers))
	}
	if consumers[0].CampaignRev == consumers[1].CampaignRev {
		t.Fatal("traced usages must reference distinct campaign revisions")
	}
}

func TestExpiredCertificateBlocksCutIn(t *testing.T) {
	plant := NewPlant(t.TempDir())
	// Issue 13 hours ago: premium certificates carry a 12 hour window.
	measurements := []feedcert.Measurement{{Component: "ethanol", Value: 0.94}}
	if _, err := plant.FeedCerts.Issue("primary", "B9", "S9", "premium", measurements, 0.02, 0.01, time.Now().Add(-13*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := plant.Start(context.Background(), "premium", "primary", "B9"); err == nil {
		t.Fatal("expired certificate must block the cut-in")
	} else if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expiry error, got %v", err)
	}
}
