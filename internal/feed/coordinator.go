package feed

import (
	"context"
	"errors"
	"github.com/wyw14/solo-039/internal/model"
)

// Authorizer admits a feed source/batch for a target grade before it is
// switched in. A nil Authorizer leaves switching ungated.
type Authorizer interface {
	Authorize(source, batch, grade string, at int64) (Authorization, error)
}

// RejectedError marks an authorization decision that refused the switch.
type RejectedError struct{ Authorization Authorization }

func (e RejectedError) Error() string {
	if e.Authorization.Reason == "" {
		return "feed batch not admitted for target grade"
	}
	return e.Authorization.Reason
}

type Transition struct {
	Stage     string
	OldSource string
	OldOpen   bool
	OldFlow   float64
	NewSource string
	NewOpen   bool
	Batch     string
}

type Coordinator struct {
	Current  *Route
	Previous *Route
	Observe  func(Transition)
	Gate     Authorizer
	// LastAuthorization is the most recent admission decision, including
	// refusals recorded by SafeSwitch.
	LastAuthorization Authorization
}

func NewCoordinator() *Coordinator { return &Coordinator{Current: &Route{}, Previous: &Route{}} }

// Switch isolates the old route and activates the new batch. Admission is
// evaluated first; when refused no route state changes and the old feed keeps
// running.
func (c *Coordinator) Switch(ctx context.Context, source, batch string, campaign model.Campaign) (Authorization, error) {
	auth := Authorization{}
	if c.Gate != nil {
		decision, err := c.Gate.Authorize(source, batch, campaign.Grade, nowUnix())
		if err != nil {
			c.LastAuthorization = Authorization{}
			return Authorization{}, err
		}
		auth = decision
		c.LastAuthorization = auth
		if !auth.Allowed {
			return auth, RejectedError{Authorization: auth}
		}
	}
	old := c.Current
	if old.Source != "" {
		if err := old.Close(ctx); err != nil {
			return auth, err
		}
		c.emit("old-isolated", old, nil)
	}
	c.Previous = old
	c.Current = &Route{}
	c.Current.Prepare(source, batch, auth)
	if err := c.Current.Activate(); err != nil {
		return auth, err
	}
	c.emit("new-active", old, c.Current)
	return auth, nil
}

func (c *Coordinator) emit(stage string, old, next *Route) {
	if c.Observe == nil {
		return
	}
	value := Transition{Stage: stage}
	if old != nil {
		value.OldSource = old.Source
		value.OldOpen = old.Open
		value.OldFlow = old.Flow
	}
	if next != nil {
		value.NewSource = next.Source
		value.NewOpen = next.Open
		value.Batch = next.Batch
	}
	c.Observe(value)
}

// SafeSwitch additionally requires the currently running source to be fully
// isolated before the cut-over.
func (c *Coordinator) SafeSwitch(ctx context.Context, source, batch string, campaign model.Campaign) (Authorization, error) {
	if c.Current.Source != "" && !c.Current.Isolated() {
		return Authorization{}, errors.New("old source is not isolated")
	}
	return c.Switch(ctx, source, batch, campaign)
}
