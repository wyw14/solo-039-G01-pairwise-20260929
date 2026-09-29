package api

import (
	"net/http"
	"time"

	"github.com/wyw14/solo-039/internal/feed"
)

func (s *Server) issueCertificate(w http.ResponseWriter, r *http.Request) {
	var input certificateRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if input.Assay.SampledAt == 0 {
		input.Assay.SampledAt = time.Now().Unix()
	}
	cert, err := s.plant.IssueFeedCertificate(input.Source, input.Batch, input.Grade, feed.Assay{
		Components:  input.Assay.Components,
		Water:       input.Assay.Water,
		Impurities:  input.Assay.Impurities,
		SampleBatch: input.Assay.SampleBatch,
		SampledAt:   input.Assay.SampledAt,
	}, input.ValidUntil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, cert)
}

func (s *Server) listCertificates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items":   s.plant.FeedCertificates(),
		"current": s.plant.CurrentFeedAuthorization(),
		"now":     time.Now().Unix(),
	})
}

func (s *Server) reinspectBatch(w http.ResponseWriter, r *http.Request) {
	var input reinspectRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cert, err := s.plant.ReinspectFeedBatch(input.Batch, input.SampleBatch, input.Passed, input.Reason)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	status := http.StatusAccepted
	writeJSON(w, status, map[string]any{
		"batch":        cert.Batch,
		"reinspection": cert.Reinspection,
		"blocked":      cert.Reinspection != nil && !cert.Reinspection.Passed,
	})
}

func (s *Server) registerGradeSpec(w http.ResponseWriter, r *http.Request) {
	var input gradeSpecRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	spec := feed.GradeSpec{
		Grade:           input.Grade,
		MinComponent:    input.MinComponents,
		MaxComponent:    input.MaxComponents,
		MaxWater:        input.MaxWater,
		MaxImpurities:   input.MaxImpurities,
		MaxAllowableQty: input.MaxAllowableQty,
	}
	if err := s.plant.RegisterGradeSpec(spec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"grade": spec.Grade, "spec": spec})
}

func (s *Server) traceFeedBatch(w http.ResponseWriter, r *http.Request) {
	batch := r.URL.Query().Get("batch")
	if batch == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "batch is required"})
		return
	}
	uses := s.plant.TraceFeedBatch(batch)
	cert, ok := s.plant.FeedRegistry.Get(batch)
	writeJSON(w, http.StatusOK, map[string]any{
		"batch":               batch,
		"certificate":         cert,
		"certificate_present": ok,
		"production_batches":  uses,
		"affected_count":      len(uses),
	})
}
