package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

type transitionRequest struct {
	Grade  string `json:"grade"`
	Source string `json:"source"`
	Batch  string `json:"batch"`
}
type assayInput struct {
	Components  map[string]float64 `json:"components"`
	Water       float64            `json:"water"`
	Impurities  float64            `json:"impurities"`
	SampleBatch string             `json:"sample_batch"`
	SampledAt   int64              `json:"sampled_at"`
}
type certificateRequest struct {
	Source     string     `json:"source"`
	Batch      string     `json:"batch"`
	Grade      string     `json:"grade"`
	ValidUntil int64      `json:"valid_until"`
	Assay      assayInput `json:"assay"`
}
type reinspectRequest struct {
	Batch       string `json:"batch"`
	SampleBatch string `json:"sample_batch"`
	Passed      bool   `json:"passed"`
	Reason      string `json:"reason"`
}
type gradeSpecRequest struct {
	Grade           string             `json:"grade"`
	MinComponents   map[string]float64 `json:"min_components"`
	MaxComponents   map[string]float64 `json:"max_components"`
	MaxWater        float64            `json:"max_water"`
	MaxImpurities   float64            `json:"max_impurities"`
	MaxAllowableQty float64            `json:"max_allowable_qty"`
}
type sampleRequest struct {
	Value float64 `json:"value"`
	At    int64   `json:"at"`
}
type routeRequest struct {
	Target string `json:"target"`
}
type leaseRequest struct {
	Owner  string  `json:"owner"`
	Amount float64 `json:"amount"`
}

func decodeJSON(r *http.Request, value any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	return nil
}

func boolQuery(r *http.Request, key string) (bool, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New("invalid boolean query")
	}
	return value, nil
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}
