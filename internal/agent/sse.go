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

// sseConfigListener connects to the SSE endpoint for config-update events and
// triggers config re-fetch on new events. It reconnects automatically with
// exponential backoff.
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
			klog.Warningf("SSE config connection error: %v", err)
		}

		select {
		case <-a.ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, sseReconnectMax)
	}
}

// sseRestartListener connects to the SSE endpoint for vGPU restart events and
// routes them to the reconciler. It runs on a dedicated connection so that
// messages can be attributed to the restart topic without relying on the
// broker setting the SSE `event:` field (which sse.tensor-fusion.ai does not).
func (a *Agent) sseRestartListener() {
	defer a.wg.Done()

	backoff := sseReconnectMin

	for {
		select {
		case <-a.ctx.Done():
			return
		default:
		}

		err := a.listenSSERestart()
		if err != nil {
			klog.Warningf("SSE restart connection error: %v", err)
		}

		select {
		case <-a.ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, sseReconnectMax)
	}
}

// listenSSE opens a single SSE connection for config-update events (topic =
// agentID) and triggers a debounced pullConfig on every received frame.
func (a *Agent) listenSSE() error {
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseEndpoint, nil)
	if err != nil {
		return err
	}
	// Subscribe only to the config-update topic.
	// Restart events are handled by a separate connection (listenSSERestart)
	// because the SSE broker does not include the topic name in the frame,
	// making it impossible to route events from a multi-topic subscription.
	req.Header.Set(sseTopicHeader, a.agentID)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		klog.Warningf("SSE config endpoint returned status %d", resp.StatusCode)
		return nil
	}

	klog.Infof("SSE config connection established: topic=%s", a.agentID)

	scanner := bufio.NewScanner(resp.Body)
	var debounceTimer *time.Timer
	var eventDataLines []string

	flushEvent := func() {
		if len(eventDataLines) == 0 {
			return
		}
		eventDataLines = nil
		// Debounce config pull so event bursts result in one pullConfig call.
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.AfterFunc(sseDebounceDelay, func() {
			klog.Infof("SSE config event received, triggering config re-fetch")
			if err := a.pullConfig(); err != nil {
				klog.Errorf("Failed to pull config after SSE event: %v", err)
			}
		})
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

	klog.Infof("SSE config connection closed by server, will reconnect")
	return nil
}

// listenSSERestart opens a single SSE connection for vGPU restart events
// (topic = agentID + "_vgpu_restart"). Every received frame is forwarded
// directly to handleVGPURestartEvent.
func (a *Agent) listenSSERestart() error {
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseEndpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set(sseTopicHeader, a.vgpuRestartTopic())
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		klog.Warningf("SSE restart endpoint returned status %d", resp.StatusCode)
		return nil
	}

	klog.Infof("SSE restart connection established: topic=%s", a.vgpuRestartTopic())

	scanner := bufio.NewScanner(resp.Body)
	var eventDataLines []string

	flushEvent := func() {
		if len(eventDataLines) == 0 {
			return
		}
		a.handleVGPURestartEvent(eventDataLines)
		eventDataLines = nil
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := scanner.Text()
		switch {
		case line == "":
			flushEvent()
		case strings.HasPrefix(line, "data:"):
			eventDataLines = append(eventDataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flushEvent()

	if err := scanner.Err(); err != nil {
		return err
	}

	klog.Infof("SSE restart connection closed by server, will reconnect")
	return nil
}

func (a *Agent) vgpuRestartTopic() string {
	return a.agentID + sseVGPURestartTopicSuffix
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

	// Sync config before restarting to ensure we use the latest license
	// Skip if client is not available (e.g., in tests)
	if a.client != nil {
		klog.Infof("Syncing config before worker restart to get latest license")
		if err := a.pullConfig(); err != nil {
			klog.Errorf("Failed to sync config before worker restart: %v", err)
			// Continue with restart anyway - better to restart with old config than not restart at all
		}
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
