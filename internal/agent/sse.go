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
	sseEndpoint       = "https://sse.tensor-fusion.ai/stream"
	sseReconnectMin   = 1 * time.Second
	sseReconnectMax   = 30 * time.Second
	sseDebounceDelay  = 500 * time.Millisecond
	sseTopicHeader    = "x-sse-topic"
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
		backoff = backoff * 2
		if backoff > sseReconnectMax {
			backoff = sseReconnectMax
		}
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
	req.Header.Set(sseTopicHeader, a.agentID)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		klog.Warningf("SSE endpoint returned status %d", resp.StatusCode)
		return nil
	}

	klog.Infof("SSE connection established: agent_id=%s", a.agentID)

	scanner := bufio.NewScanner(resp.Body)
	var debounceTimer *time.Timer

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

		// SSE protocol: lines starting with "data:" contain event data
		if strings.HasPrefix(line, "data:") {
			// Debounce: reset timer on each data line so rapid events
			// result in a single pullConfig call
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(sseDebounceDelay, func() {
				klog.Infof("SSE event received, triggering config re-fetch")
				if err := a.pullConfig(); err != nil {
					klog.Errorf("Failed to pull config after SSE event: %v", err)
				}
			})
		}
	}

	if debounceTimer != nil {
		debounceTimer.Stop()
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	klog.Infof("SSE connection closed by server, will reconnect")
	return nil
}
