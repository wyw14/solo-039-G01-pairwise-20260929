package feedcert

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testSpec() GradeSpec {
	return GradeSpec{
		Grade:        "premium",
		Components:   map[string]Range{"ethanol": {Min: 0.92, Max: 0.97}},
		MaxWater:     0.04,
		MaxImpurity:  0.015,
		CertTTLHours: 12,
	}
}

func TestIssueValidates(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if _, err := NewCertificate("", "B1", "S1", "premium", []Measurement{{"ethanol", 0.94}}, 0.02, 0.01, now, testSpec()); err == nil {
		t.Fatal("expected error for missing source")
	}
	if _, err := NewCertificate("tank-A", "B1", "", "premium", []Measurement{{"ethanol", 0.94}}, 0.02, 0.01, now, testSpec()); err == nil {
		t.Fatal("expected error for missing sample batch")
	}
	if _, err := NewCertificate("tank-A", "B1", "S1", "premium", nil, 0.02, 0.01, now, testSpec()); err == nil {
		t.Fatal("expected error for empty measurements")
	}
	if _, err := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", -0.1}}, 0.02, 0.01, now, testSpec()); err == nil {
		t.Fatal("expected error for negative measurement")
	}
	if _, err := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"", 0.94}}, 0.02, 0.01, now, testSpec()); err == nil {
		t.Fatal("expected error for unnamed component")
	}
}

func TestEvaluateFitLot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cert, err := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", 0.94}}, 0.02, 0.01, now, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	decision := Evaluate(cert, testSpec(), now)
	if !decision.Allowed {
		t.Fatalf("expected lot allowed: %v", decision.Reasons)
	}
	if decision.UsableAmount <= 0 || decision.UsableAmount > 1 {
		t.Fatalf("usable amount out of bounds: %f", decision.UsableAmount)
	}
}

func TestEvaluateOutOfRange(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	// Ethanol 0.90 is below the premium minimum 0.92.
	cert, _ := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", 0.90}}, 0.02, 0.01, now, testSpec())
	decision := Evaluate(cert, testSpec(), now)
	if decision.Allowed {
		t.Fatal("out-of-range lot must not be allowed")
	}
	if decision.UsableAmount != 0 {
		t.Fatalf("blocked lot usable amount must be 0, got %f", decision.UsableAmount)
	}
	if len(decision.Violations) != 1 || decision.Violations[0].Kind != "component" {
		t.Fatalf("expected one component violation, got %+v", decision.Violations)
	}
}

func TestEvaluateWaterImpurityLimits(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cert, _ := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", 0.94}}, 0.05, 0.02, now, testSpec())
	decision := Evaluate(cert, testSpec(), now)
	if decision.Allowed {
		t.Fatal("over-limit lot must be blocked")
	}
	kinds := map[string]bool{}
	for _, v := range decision.Violations {
		kinds[v.Kind] = true
	}
	if !kinds["water"] || !kinds["impurity"] {
		t.Fatalf("expected water and impurity violations, got %+v", decision.Violations)
	}
}

func TestEvaluateMissingRequiredComponent(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	spec := testSpec()
	spec.Components["methanol"] = Range{Min: 0, Max: 0.005}
	cert, _ := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", 0.94}}, 0.02, 0.01, now, spec)
	// The certificate was stamped against the original spec; re-judge against
	// a spec demanding methanol, which was not measured.
	decision := Evaluate(cert, spec, now)
	if decision.Allowed {
		t.Fatal("lot missing a required component must be blocked")
	}
	found := false
	for _, v := range decision.Violations {
		if v.Kind == "missing-component" && v.Name == "methanol" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing-component violation for methanol, got %+v", decision.Violations)
	}
}

func TestEvaluateExpired(t *testing.T) {
	issued := time.Unix(1_700_000_000, 0)
	cert, _ := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", 0.94}}, 0.02, 0.01, issued, testSpec())
	later := issued.Add(13 * time.Hour)
	decision := Evaluate(cert, testSpec(), later)
	if decision.Allowed {
		t.Fatal("expired certificate must block the lot")
	}
	if !strings.Contains(decision.Reasons[0], "expired") {
		t.Fatalf("expected expiry reason, got %q", decision.Reasons[0])
	}
}

func TestEvaluateRecheckFailureBlocks(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cert, _ := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", 0.94}}, 0.02, 0.01, now, testSpec())
	cert.RecheckFailed = true
	decision := Evaluate(cert, testSpec(), now)
	if decision.Allowed {
		t.Fatal("failed re-inspection must block")
	}
	if !strings.Contains(decision.Reasons[0], "re-inspection") {
		t.Fatalf("expected recheck reason, got %q", decision.Reasons[0])
	}
}

func TestRegistryIssueJudgeRecheck(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterSpec(testSpec()); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)

	if _, _, err := registry.Judge("tank-A", "B1", "premium", now); err == nil {
		t.Fatal("judging an unknown batch must fail")
	}

	cert, err := registry.Issue("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", 0.94}}, 0.02, 0.01, now)
	if err != nil {
		t.Fatal(err)
	}
	decision, stored, err := registry.Judge("tank-A", "B1", "premium", now)
	if err != nil || !decision.Allowed {
		t.Fatalf("expected fit verdict, got %v %v", decision, err)
	}
	if stored.ID != cert.ID {
		t.Fatal("judge must use the newest certificate")
	}

	// Re-inspection fails: the same batch must be rejected until a new
	// certificate is issued for a fresh sampled lot.
	if _, err := registry.FailRecheck("tank-A", "B1", now.Add(time.Hour), "off-spec methanol"); err != nil {
		t.Fatal(err)
	}
	decision, _, err = registry.Judge("tank-A", "B1", "premium", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal("judge returns the verdict, not an error, for a rejected lot")
	}
	if decision.Allowed {
		t.Fatal("recheck failure must block new cut-in")
	}

	// New sampled lot, new certificate clears the block.
	cert2, err := registry.Issue("tank-A", "B1", "S2", "premium", []Measurement{{"ethanol", 0.945}}, 0.02, 0.01, now.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	decision, stored, _ = registry.Judge("tank-A", "B1", "premium", now.Add(3*time.Hour))
	if !decision.Allowed || stored.ID != cert2.ID {
		t.Fatalf("new certificate must restore fitness: %+v", decision)
	}
}

func TestUsableAmountShrinksAsMeasurementNearsLimit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	spec := testSpec()
	fresh, _ := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", 0.93}}, 0.005, 0.002, now, spec)
	nearLimit, _ := NewCertificate("tank-A", "B2", "S2", "premium", []Measurement{{"ethanol", 0.93}}, 0.039, 0.002, now, spec)
	decisionFresh := Evaluate(fresh, spec, now)
	decisionNear := Evaluate(nearLimit, spec, now)
	if !decisionFresh.Allowed || !decisionNear.Allowed {
		t.Fatalf("both lots fit: %v %v", decisionFresh.Reasons, decisionNear.Reasons)
	}
	if decisionNear.UsableAmount >= decisionFresh.UsableAmount {
		t.Fatalf("lot near water limit must have smaller usable amount: fresh=%f near=%f", decisionFresh.UsableAmount, decisionNear.UsableAmount)
	}
}

func TestUsageLedgerTrace(t *testing.T) {
	ledger := NewUsageLedger()
	cert, _ := NewCertificate("tank-A", "B1", "S1", "premium", []Measurement{{"ethanol", 0.94}}, 0.02, 0.01, time.Unix(1_700_000_000, 0), testSpec())
	campaign1 := uuid.New()
	campaign2 := uuid.New()
	ledger.Record(Usage{FeedSource: "tank-A", FeedBatch: "B1", CertificateID: cert.ID, CampaignID: campaign1, Grade: "premium", UsableAmount: 0.5})
	ledger.Record(Usage{FeedSource: "tank-A", FeedBatch: "B1", CertificateID: cert.ID, CampaignID: campaign2, Grade: "premium", UsableAmount: 0.6})
	ledger.Record(Usage{FeedSource: "tank-B", FeedBatch: "B2", CertificateID: cert.ID, CampaignID: campaign2, Grade: "premium", UsableAmount: 0.6})

	traced := ledger.ForFeedBatch("tank-A", "B1")
	if len(traced) != 2 {
		t.Fatalf("expected 2 production batches traced, got %d", len(traced))
	}
	if traced[0].CampaignID == traced[1].CampaignID {
		t.Fatal("expected distinct campaigns")
	}
	if len(ledger.ForCampaign(campaign2)) != 2 {
		t.Fatal("campaign c2 used two feed batches")
	}
}

func TestDefaultSpecs(t *testing.T) {
	registry := NewRegistry()
	RegisterDefaults(registry)
	if _, ok := registry.Spec("baseline"); !ok {
		t.Fatal("baseline spec must be seeded")
	}
	if _, ok := registry.Spec("premium"); !ok {
		t.Fatal("premium spec must be seeded")
	}
}
