package feed

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func sampleAssay() Assay {
	return Assay{
		Components:  map[string]float64{"ethanol": 0.92},
		Water:       0.05,
		Impurities:  0.01,
		SampleBatch: "SAMPLE-1",
		SampledAt:   1000,
	}
}

func TestEvaluateAdmitsWithinGradeRange(t *testing.T) {
	registry := NewRegistry()
	cert, err := NewCertificate("tank-A", "BATCH-1", "premium", sampleAssay(), 2000)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Issue(cert); err != nil {
		t.Fatal(err)
	}
	auth, err := registry.Authorize("tank-A", "BATCH-1", "premium", 1500)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.Allowed {
		t.Fatalf("expected admission, got %q", auth.Reason)
	}
	if auth.AllowedQty != 60 {
		t.Fatalf("allowed qty = %v, want 60", auth.AllowedQty)
	}
	if auth.SampleBatch != "SAMPLE-1" {
		t.Fatal("sample batch missing from authorization")
	}
}

func TestEvaluateRejectsOutOfRangeAndStaleCertificates(t *testing.T) {
	registry := NewRegistry()
	cases := []struct {
		name   string
		mutate func(*Assay)
		grade  string
		at     int64
		reject func(Certificate) Certificate
		want   string
	}{
		{name: "expired", grade: "premium", at: 2500, want: "expired"},
		{name: "water over limit", grade: "premium", at: 1500, mutate: func(a *Assay) { a.Water = 0.20 }, want: "water"},
		{name: "impurities over limit", grade: "premium", at: 1500, mutate: func(a *Assay) { a.Impurities = 0.05 }, want: "impurity"},
		{name: "component below minimum", grade: "premium", at: 1500, mutate: func(a *Assay) { a.Components["ethanol"] = 0.85 }, want: "minimum"},
		{name: "wrong grade", grade: "baseline", at: 1500, want: "not issued"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assay := sampleAssay()
			if tc.mutate != nil {
				tc.mutate(&assay)
			}
			cert, err := NewCertificate("tank-A", "BATCH-"+tc.name, "premium", assay, 2000)
			if err != nil {
				t.Fatal(err)
			}
			if tc.reject != nil {
				cert = tc.reject(cert)
			}
			if err := registry.Issue(cert); err != nil {
				t.Fatal(err)
			}
			spec := registry.specs[tc.grade]
			auth := cert.Evaluate(spec, tc.at)
			if auth.Allowed || auth.AllowedQty != 0 {
				t.Fatalf("expected rejection, admitted: %+v", auth)
			}
			if auth.Reason == "" {
				t.Fatal("rejection must carry a reason")
			}
		})
	}
}

func TestFailedReinspectionBlocksAuthorization(t *testing.T) {
	registry := NewRegistry()
	cert, err := NewCertificate("tank-A", "BATCH-R", "premium", sampleAssay(), time.Now().Unix()+3600)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Issue(cert); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Reinspect("BATCH-R", "SAMPLE-R2", false, "sediment", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	auth, err := registry.Authorize("tank-A", "BATCH-R", "premium", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if auth.Allowed {
		t.Fatal("rejected reinspection must block switch-in")
	}

	// A passing re-test clears the block.
	if _, err := registry.Reinspect("BATCH-R", "SAMPLE-R3", true, "", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	auth, err = registry.Authorize("tank-A", "BATCH-R", "premium", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if !auth.Allowed {
		t.Fatalf("passing reinspection should re-admit, got %q", auth.Reason)
	}
}

func TestAuthorizeRejectsSourceMismatchAndUnknownBatch(t *testing.T) {
	registry := NewRegistry()
	cert, _ := NewCertificate("tank-A", "BATCH-1", "premium", sampleAssay(), time.Now().Unix()+3600)
	_ = registry.Issue(cert)
	if _, err := registry.Authorize("tank-B", "BATCH-1", "premium", time.Now().Unix()); err == nil {
		t.Fatal("source mismatch must be refused")
	}
	if _, err := registry.Authorize("tank-A", "MISSING", "premium", time.Now().Unix()); err == nil {
		t.Fatal("unknown batch must be refused")
	}
	if _, err := registry.Authorize("tank-A", "BATCH-1", "unlisted", time.Now().Unix()); err == nil {
		t.Fatal("unregistered grade must be refused")
	}
}

func TestNewCertificateValidatesAssay(t *testing.T) {
	base := sampleAssay()
	validUntil := time.Now().Unix() + 3600
	cases := []struct {
		name string
		edit func(*Assay)
	}{
		{"missing sample batch", func(a *Assay) { a.SampleBatch = "" }},
		{"missing components", func(a *Assay) { a.Components = nil }},
		{"water out of range", func(a *Assay) { a.Water = 1.5 }},
		{"impurities negative", func(a *Assay) { a.Impurities = -0.1 }},
		{"components exceed total", func(a *Assay) { a.Components = map[string]float64{"ethanol": 0.9, "methanol": 0.2} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assay := base
			tc.edit(&assay)
			if _, err := NewCertificate("tank-A", "BATCH-X", "premium", assay, validUntil); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if _, err := NewCertificate("tank-A", "BATCH-X", "premium", base, base.SampledAt-1); err == nil {
		t.Fatal("validity before sampling must be rejected")
	}
}

func TestTraceLinksBatchToProductionCampaigns(t *testing.T) {
	registry := NewRegistry()
	cert, _ := NewCertificate("tank-A", "BATCH-T", "premium", sampleAssay(), time.Now().Unix()+3600)
	_ = registry.Issue(cert)
	registry.RecordUsage(FeedUse{Batch: "BATCH-T", Certificate: cert.ID, Campaign: uuid.New(), Grade: "premium", At: 10})
	registry.RecordUsage(FeedUse{Batch: "OTHER", Campaign: uuid.New(), At: 11})
	registry.RecordUsage(FeedUse{Batch: "BATCH-T", Certificate: cert.ID, Campaign: uuid.New(), Grade: "premium", At: 12})

	uses := registry.Trace("BATCH-T")
	if len(uses) != 2 {
		t.Fatalf("trace = %d uses, want 2", len(uses))
	}
	if uses[0].At > uses[1].At {
		t.Fatal("trace must be time ordered")
	}
}
