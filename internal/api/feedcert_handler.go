package api

import (
	"net/http"

	"github.com/wyw14/solo-039/internal/service"
)

func (s *Server) issueFeedCertificate(w http.ResponseWriter, r *http.Request) {
	var input service.FeedCertificateInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cert, err := s.plant.IssueFeedCertificate(input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, cert)
}

func (s *Server) failFeedRecheck(w http.ResponseWriter, r *http.Request) {
	var input recheckRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := s.plant.FailFeedRecheck(input.Source, input.Batch, input.Reason)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// 409, not 200: the lot is now quarantined and the body lists the
	// production batches already exposed to it.
	writeJSON(w, http.StatusConflict, result)
}

func (s *Server) listFeedCertificates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.plant.ListFeedCertificates()})
}

func (s *Server) traceFeedBatch(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	batch := r.URL.Query().Get("batch")
	if source == "" || batch == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source and batch are required"})
		return
	}
	consumers, err := s.plant.TraceFeedBatch(source, batch)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": source, "batch": batch, "production_batches": consumers})
}
