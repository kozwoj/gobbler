// runner is a CLI tool that generates random test items and sends them to a
// Gobbler server in batches.
//
// The server must already be configured (pipeline/configure called) before
// running. The runner registers item type definitions, starts the pipeline,
// sends batches on a ticker, then stops the pipeline on exit.
//
// Run from the repo root:
//
//	go run ./tester/runner -url http://localhost:8080 -types alpha,beta,gamma
//	go run ./tester/runner -url http://localhost:8080 -types alpha,beta,gamma -batch 10 -interval 1s
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/kozwoj/gobbler/tester"
)

func main() {
	urlFlag := flag.String("url", "http://localhost:8080", "Base URL of the Gobbler server")
	typesFlag := flag.String("types", "alpha,beta,gamma", "Comma-separated item type names to generate")
	batchFlag := flag.Int("batch", 10, "Items per request")
	intervalFlag := flag.Duration("interval", time.Second, "Pause between requests")
	totalFlag := flag.Int("total", 0, "Stop after N items sent; 0 = unlimited")
	seedFlag := flag.Int64("seed", 0, "RNG seed; 0 = time-based")
	flag.Parse()

	// Build RNG.
	seed := *seedFlag
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	// Parse and validate type names.
	rawNames := strings.Split(*typesFlag, ",")
	typeNames := make([]string, 0, len(rawNames))
	for _, n := range rawNames {
		n = strings.TrimSpace(n)
		if n != "" {
			typeNames = append(typeNames, n)
		}
	}
	if len(typeNames) == 0 {
		log.Fatal("error: no types specified")
	}

	// Build generators.
	generators := make([]tester.ItemGenerator, 0, len(typeNames))
	for _, name := range typeNames {
		gen, err := tester.NewGenerator(name, rng)
		if err != nil {
			log.Fatalf("error: %v", err)
		}
		generators = append(generators, gen)
	}

	base := strings.TrimRight(*urlFlag, "/")

	// 1. Verify pipeline is configured but not yet running.
	if err := checkStatus(base); err != nil {
		log.Fatalf("pre-flight check failed: %v", err)
	}
	log.Println("verified: pipeline configured and not running")

	// 2. Register item type definitions.
	for _, name := range typeNames {
		if err := registerDefinition(base, name); err != nil {
			log.Fatalf("failed to register definition for %q: %v", name, err)
		}
	}
	log.Printf("registered %d definition(s): %s", len(typeNames), strings.Join(typeNames, ", "))

	// 3. Start pipeline.
	if err := postNoBody(base + "/gobbler/pipeline/start"); err != nil {
		log.Fatalf("failed to start pipeline: %v", err)
	}
	log.Println("pipeline started")

	// Ensure pipeline is always stopped on exit.
	pipelineStarted := true
	defer func() {
		if !pipelineStarted {
			return
		}
		if err := postNoBody(base + "/gobbler/pipeline/stop"); err != nil {
			log.Printf("warning: failed to stop pipeline: %v", err)
		} else {
			log.Println("pipeline stopped")
		}
	}()

	// 4. Handle Ctrl-C / SIGTERM gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// 5. Send batches.
	ingestURL := base + "/gobbler/ingest"
	ticker := time.NewTicker(*intervalFlag)
	defer ticker.Stop()

	log.Printf("sending — types=[%s] batch=%d interval=%s total=%d (0=unlimited)",
		strings.Join(typeNames, ","), *batchFlag, *intervalFlag, *totalFlag)

	sent := 0
	batchNum := 0

	for {
		select {
		case <-ctx.Done():
			log.Println("interrupted; shutting down")
			return
		case <-ticker.C:
			items := make([]map[string]any, *batchFlag)
			for i := range items {
				gen := generators[rng.Intn(len(generators))]
				items[i] = gen.GenerateWrapped()
			}
			if err := sendBatch(ingestURL, items); err != nil {
				log.Printf("ingest error: %v", err)
				continue
			}
			batchNum++
			sent += *batchFlag
			log.Printf("batch %d — %d items sent (total: %d)", batchNum, *batchFlag, sent)

			if *totalFlag > 0 && sent >= *totalFlag {
				log.Printf("reached total limit (%d items)", *totalFlag)
				return
			}
		}
	}
}

func checkStatus(base string) error {
	resp, err := http.Get(base + "/gobbler/pipeline/status")
	if err != nil {
		return fmt.Errorf("GET /gobbler/pipeline/status: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var status struct {
		Configured bool `json:"configured"`
		Running    bool `json:"running"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return fmt.Errorf("parse status response: %w", err)
	}
	if !status.Configured {
		return fmt.Errorf("pipeline not configured; call pipeline/configure first")
	}
	if status.Running {
		return fmt.Errorf("pipeline is already running; stop it before starting the runner")
	}
	return nil
}

func registerDefinition(base, typeName string) error {
	defJSON, ok := tester.DefinitionJSON(typeName)
	if !ok {
		return fmt.Errorf("no embedded definition found for type %q", typeName)
	}
	resp, err := http.Post(base+"/gobbler/definition/add", "application/json", bytes.NewReader(defJSON))
	if err != nil {
		return fmt.Errorf("POST /gobbler/definition/add: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func postNoBody(url string) error {
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte{}))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func sendBatch(url string, items []map[string]any) error {
	payload, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("POST /gobbler/ingest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
