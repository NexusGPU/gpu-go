/*
Copyright 2024.
Licensed under the Apache License, Version 2.0 (the "License");
*/

package api

// HTTP API Response Types

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// DataResponse is a generic response wrapper for data-only responses
type DataResponse[T any] struct {
	Data T `json:"data"`
}

// MessageAndDataResponse is a generic response wrapper for responses with message and data
type MessageAndDataResponse[T any] struct {
	Message string `json:"message"`
	Data    T      `json:"data"`
}

// StatusResponse represents a simple status response
type StatusResponse struct {
	Status string `json:"status"`
}

// Types to be compatible with legacy APIs

// LimiterInfo represents worker limiter information
type LimiterInfo struct {
	WorkerUID string    `json:"worker_uid"`
	Requests  *Resource `json:"requests,omitempty"`
	Limits    *Resource `json:"limits,omitempty"`
}

// ListLimitersResponse represents the response from GET /api/v1/limiter
type ListLimitersResponse struct {
	Limiters []LimiterInfo `json:"limiters"`
}

// TrapResponse represents the response from POST /api/v1/trap
type TrapResponse struct {
	Message       string `json:"message"`
	SnapshotCount int    `json:"snapshot_count"`
}

// PodInfo represents pod information for the /api/v1/pod endpoint
type PodInfo struct {
	PodName     string   `json:"pod_name"`
	Namespace   string   `json:"namespace"`
	GPUIDs      []string `json:"gpu_uuids"`
	TflopsLimit *float64 `json:"tflops_limit,omitempty"`
	VramLimit   *uint64  `json:"vram_limit,omitempty"`
	QoSLevel    QoSLevel `json:"qos_level,omitempty"`
}

// ListPodsResponse represents the response from GET /api/v1/pod
type ListPodsResponse struct {
	Pods []PodInfo `json:"pods"`
}

// ProcessInfo represents process mapping information
type ProcessInfo struct {
	WorkerUID      string            `json:"worker_uid"`
	ProcessMapping map[string]string `json:"process_mapping"` // container PID -> host PID
}

// ListProcessesResponse represents the response from GET /api/v1/process
type ListProcessesResponse struct {
	Processes []ProcessInfo `json:"processes"`
}
