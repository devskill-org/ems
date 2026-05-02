package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/devskill-org/ems/mpc"
)

// dataServiceClient handles communication with the data-service HTTP server
// for storing and retrieving MPC decisions.
type dataServiceClient struct {
	baseURL string
	client  *http.Client
}

// newDataServiceClient creates a new data-service client.
// Returns nil if baseURL is empty.
func newDataServiceClient(config *Config) *dataServiceClient {
	if config.DataServiceURL == "" {
		return nil
	}
	return &dataServiceClient{
		baseURL: config.DataServiceURL,
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// saveMPCDecisions sends decisions to the data-service via its POST /mpc/save endpoint.
// The data-service replaces the full in-memory list with the provided decisions.
func (s *MinerScheduler) saveMPCDecisions(ctx context.Context, decisions []mpc.ControlDecision) error {
	if s.dataServiceClient == nil {
		return nil // data-service not configured, skip storage
	}
	if len(decisions) == 0 {
		return nil
	}

	body, err := json.Marshal(decisions)
	if err != nil {
		return fmt.Errorf("failed to marshal decisions: %w", err)
	}

	url := s.dataServiceClient.baseURL + "/mpc/save"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.dataServiceClient.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to POST to data-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("data-service POST /mpc/save returned %d: %s", resp.StatusCode, string(respBody))
	}

	_, _ = io.Copy(io.Discard, resp.Body) // drain body to reuse connection
	s.logger.Printf("Saved %d MPC decisions to data-service", len(decisions))
	return nil
}

// loadLatestMPCDecisions fetches the current list of MPC decisions from the
// data-service via its GET /mpc/get endpoint.  Unlike the previous DB variant
// this does NOT filter by timestamp — the data-service holds decisions for all
// hours; the scheduler can filter client-side if needed.
func (s *MinerScheduler) loadLatestMPCDecisions(ctx context.Context) ([]mpc.ControlDecision, error) {
	if s.dataServiceClient == nil {
		return nil, nil // data-service not configured
	}
	url := s.dataServiceClient.baseURL + "/mpc/get"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.dataServiceClient.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to GET from data-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("data-service GET /mpc/get returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Load decisions
	decisions, err := loadDecisionsFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode MPC decisions: %w", err)
	}

	if len(decisions) == 0 {
		s.logger.Printf("No MPC decisions found in data-service")
		return nil, nil
	}

	s.logger.Printf("Loaded %d MPC decisions from data-service", len(decisions))
	return decisions, nil
}

// loadDecisionsFromReader decodes MPC decisions from an io.Reader.
func loadDecisionsFromReader(r io.Reader) ([]mpc.ControlDecision, error) {
	var decisions []mpc.ControlDecision
	err := json.NewDecoder(r).Decode(&decisions)
	if err != nil {
		return nil, err
	}
	return decisions, nil
}
