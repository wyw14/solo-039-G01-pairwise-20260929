package feed

import (
	"context"
	"errors"
	"github.com/wyw14/solo-039/internal/model"
	"testing"
)

func TestSwitchClosesPreviousRoute(t *testing.T) {
	coordinator := NewCoordinator()
	coordinator.Current.Prepare("A", "BATCH-A", Authorization{})
	if err := coordinator.Current.Activate(); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Switch(context.Background(), "B", "BATCH-B", model.NewCampaign("grade")); err != nil {
		t.Fatal(err)
	}
	if !coordinator.Previous.Isolated() {
		t.Fatal("previous route remains active")
	}
	if !coordinator.Current.Open {
		t.Fatal("new route is closed")
	}
	if coordinator.Current.Batch != "BATCH-B" {
		t.Fatalf("new route batch = %q, want BATCH-B", coordinator.Current.Batch)
	}
}

type stubAuthorizer struct {
	auth Authorization
	err  error
}

func (s stubAuthorizer) Authorize(source, batch, grade string, at int64) (Authorization, error) {
	return s.auth, s.err
}

func TestSwitchRejectedKeepsOldRoute(t *testing.T) {
	coordinator := NewCoordinator()
	coordinator.Current.Prepare("A", "BATCH-A", Authorization{Allowed: true})
	if err := coordinator.Current.Activate(); err != nil {
		t.Fatal(err)
	}
	rejected := Authorization{Allowed: false, Reason: "feed suitability certificate expired", Batch: "BATCH-B"}
	coordinator.Gate = stubAuthorizer{auth: rejected}

	_, err := coordinator.Switch(context.Background(), "B", "BATCH-B", model.NewCampaign("grade"))
	var refused RejectedError
	if !errors.As(err, &refused) {
		t.Fatalf("expected RejectedError, got %v", err)
	}
	if coordinator.Current.Source != "A" || !coordinator.Current.Open {
		t.Fatal("old route must remain active after rejection")
	}
	if coordinator.LastAuthorization.Allowed {
		t.Fatal("last authorization should record the refusal")
	}
}

func TestSwitchAdmittedCarriesAllowableQuantity(t *testing.T) {
	coordinator := NewCoordinator()
	coordinator.Gate = stubAuthorizer{auth: Authorization{Allowed: true, AllowedQty: 42, Batch: "BATCH-A"}}
	auth, err := coordinator.Switch(context.Background(), "A", "BATCH-A", model.NewCampaign("premium"))
	if err != nil {
		t.Fatal(err)
	}
	if auth.AllowedQty != 42 || coordinator.Current.AllowedQty != 42 {
		t.Fatal("allowed quantity not propagated to active route")
	}
}
