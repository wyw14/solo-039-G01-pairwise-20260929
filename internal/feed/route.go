package feed

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/model"
)

type Route struct {
	Source      string
	Batch       string
	Certificate uuid.UUID
	AllowedQty  float64
	Revision    uuid.UUID
	Open        bool
	Flow        float64
}

func (r *Route) Prepare(source, batch string, auth Authorization) {
	r.Source = source
	r.Batch = batch
	r.Certificate = auth.Certificate
	r.AllowedQty = auth.AllowedQty
	r.Revision = uuid.New()
	r.Open = false
	r.Flow = 0
}

func (r *Route) Isolated() bool { return !r.Open && r.Flow == 0 }

func (r *Route) Activate() error {
	if r.Source == "" {
		return errors.New("source required")
	}
	r.Open = true
	r.Flow = 1
	return nil
}

func (r *Route) Close(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	r.Open = false
	r.Flow = 0
	return nil
}

func (r *Route) Event(c model.Campaign) model.Event { return model.NewEvent("feed.route", c, r.Source) }
