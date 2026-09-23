// Package main implements the data-service HTTP server for storing and retrieving MPC control decisions.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/devskill-org/ems/mpc"
)

// defaultPort is the port used when PORT is not set in the environment.
const defaultPort = 8081

var (
	decisions   []mpc.ControlDecision
	decisionsMu sync.RWMutex
)

func main() {
	// Validate the port from the environment to avoid propagating untrusted input.
	portNum := defaultPort
	if raw := os.Getenv("PORT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			log.Fatalf("invalid PORT value")
		}
		portNum = parsed
	}
	port := strconv.Itoa(portNum)

	mux := http.NewServeMux()
	mux.HandleFunc("/mpc/save", handleMPCSave)
	mux.HandleFunc("/mpc/get", handleMPCGet)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("data-service HTTP server listening on :%d", portNum)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down data-service...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("data-service stopped")
}

func handleMPCSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var saved []mpc.ControlDecision
	if err := json.NewDecoder(r.Body).Decode(&saved); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	decisionsMu.Lock()
	decisions = saved
	decisionsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":          "ok",
		"decisions_saved": len(saved),
	}); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

func handleMPCGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	decisionsMu.RLock()
	snapshot := make([]mpc.ControlDecision, len(decisions))
	copy(snapshot, decisions)
	decisionsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}
