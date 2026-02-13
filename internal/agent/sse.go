package agent

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

const (
	sseEndpoint               = "https://sse.tensor-fusion.ai/v1/stream"
	sseReconnectMin           = 1 * time.Second
	sseReconnectMax           = 30 * time.Second
	sseDebounceDelay          = 500 * time.Millisecond
	sseTopicHeader            = "x-sse-topic"
	sseVGPURestartTopicSuffix = "_vgpu_restart"
)

// sseConfigListener connects to the SSE endpoint and triggers config re-fetch on new events.
// It reconnects automatically with exponential backoff.
func (a *Agent) sseConfigListener() {
	defer a.wg.Done()

	backoff := sseReconnectMin

	for {
		select {
		case <-a.ctx.Done():
			return
		default:
		}

		err := a.listenSSE()
		if err != nil {
			klog.Warningf("SSE connection error: %v", err)
		}

		// Check if context is done before reconnecting
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Exponential backoff
		backoff = min(backoff*2, sseReconnectMax)
	}
}

// listenSSE opens a single SSE connection and processes events until disconnected.
func (a *Agent) listenSSE() error {
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseEndpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set(sseTopicHeader, strings.Join([]string{a.agentID, a.vgpuRestartTopic()}, ","))
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		klog.Warningf("SSE endpoint returned status %d", resp.StatusCode)
		return nil
	}

	klog.Infof("SSE connection established: topics=%s,%s", a.agentID, a.vgpuRestartTopic())

	scanner := bufio.NewScanner(resp.Body)
	var debounceTimer *time.Timer
	var eventType string
	var eventDataLines []string

	flushEvent := func() {
		if len(eventDataLines) == 0 {
			eventType = ""
			return
		}
		a.handleSSEEvent(eventType, eventDataLines, &debounceTimer)
		eventType = ""
		eventDataLines = nil
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return nil
		default:
		}

		line := scanner.Text()

		switch {
		case line == "":
			flushEvent()
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			eventDataLines = append(eventDataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flushEvent()

	if debounceTimer != nil {
		debounceTimer.Stop()
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	klog.Infof("SSE connection closed by server, will reconnect")
	return nil
}

func (a *Agent) vgpuRestartTopic() string {
	return a.agentID + sseVGPURestartTopicSuffix
}

func (a *Agent) handleSSEEvent(eventType string, dataLines []string, debounceTimer **time.Timer) {
	if strings.TrimSpace(eventType) == a.vgpuRestartTopic() {
		a.handleVGPURestartEvent(dataLines)
		return
	}

	// Debounce config pull so event bursts result in one pullConfig call.
	if *debounceTimer != nil {
		(*debounceTimer).Stop()
	}
	*debounceTimer = time.AfterFunc(sseDebounceDelay, func() {
		klog.Infof("SSE event received, triggering config re-fetch")
		if err := a.pullConfig(); err != nil {
			klog.Errorf("Failed to pull config after SSE event: %v", err)
		}
	})
}

func (a *Agent) handleVGPURestartEvent(dataLines []string) int {
	workerIDs := parseVGPURestartWorkers(dataLines)
	if len(workerIDs) == 0 {
		return 0
	}
	if a.reconciler == nil {
		klog.Warningf("Received vGPU restart SSE event but reconciler is unavailable")
		return 0
	}

	queued := a.reconciler.RequestWorkerRestarts(workerIDs)
	if queued > 0 {
		klog.Infof("Queued worker restarts from SSE event: workers=%s", strings.Join(workerIDs, ","))
	}
	return queued
}

func parseVGPURestartWorkers(lines []string) []string {
	workers := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		workerID := strings.TrimSpace(line)
		if workerID == "" {
			continue
		}
		if _, exists := seen[workerID]; exists {
			continue
		}
		seen[workerID] = struct{}{}
		workers = append(workers, workerID)
	}
	return workers
}
