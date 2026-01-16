/*
Copyright 2024.
Licensed under the Apache License, Version 2.0 (the "License");
*/

package api

// IsolationMode represents the isolation mode for worker processes
type IsolationMode string

const (
	IsolationModeSoft IsolationMode = "soft"
	IsolationModeHard IsolationMode = "hard"
	IsolationModeNone IsolationMode = "none"
)

// QoSLevel represents the quality of service level
type QoSLevel string

const (
	QoSLevelLow      QoSLevel = "low"
	QoSLevelMedium   QoSLevel = "medium"
	QoSLevelHigh     QoSLevel = "high"
	QoSLevelCritical QoSLevel = "critical"
)

// Resource represents resource requests/limits
type Resource struct {
	Tflops   float64 `json:"tflops,omitempty"`
	VRAMBytes uint64  `json:"vram_bytes,omitempty"`
}

// WorkerInfo represents worker process information
// +k8s:deepcopy-gen=true
type WorkerInfo struct {
	WorkerUID        string   `json:"worker_uid"`
	Namespace        string   `json:"namespace,omitempty"`
	WorkerName       string   `json:"worker_name,omitempty"`
	AllocatedDevices []string `json:"allocated_devices"`
	Status           WorkerStatus `json:"status"`

	QoS           QoSLevel      `json:"qos,omitempty"`
	IsolationMode IsolationMode `json:"isolation_mode,omitempty"`

	Requests Resource `json:"requests,omitempty"`
	Limits   Resource `json:"limits,omitempty"`

	WorkloadName      string `json:"workload_name,omitempty"`
	WorkloadNamespace string `json:"workload_namespace,omitempty"`

	// Only set for partitioned mode
	PartitionTemplateID string `json:"partition_template_id,omitempty"`

	// Extra information from backend
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	DeletedAt int64 `json:"deleted_at,omitempty"`
}

func (w *WorkerInfo) FilterValue() string {
	return w.WorkerUID + " " + w.WorkerName + " " + w.Namespace
}

// WorkerStatus represents the status of a worker
type WorkerStatus string

const (
	WorkerStatusPending          WorkerStatus = "Pending"
	WorkerStatusDeviceAllocating WorkerStatus = "DeviceAllocating"
	WorkerStatusRunning          WorkerStatus = "Running"
	WorkerStatusTerminated       WorkerStatus = "Terminated"
)

// WorkerAllocation represents a worker allocation with device info
// +k8s:deepcopy-gen=true
type WorkerAllocation struct {
	WorkerInfo *WorkerInfo `json:"worker_info"`

	// the complete or partitioned device info
	DeviceInfos []*DeviceInfo `json:"device_infos,omitempty"`

	Envs map[string]string `json:"envs,omitempty"`

	Mounts []*Mount `json:"mounts,omitempty"`

	Devices []*DeviceSpec `json:"devices,omitempty"`
}

// DeviceSpec specifies a host device to mount into a container.
// +k8s:deepcopy-gen=true
type DeviceSpec struct {
	GuestPath   string `json:"guest_path,omitempty"`
	HostPath    string `json:"host_path,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

// Mount specifies a host volume to mount into a container.
// +k8s:deepcopy-gen=true
type Mount struct {
	GuestPath string `json:"guest_path,omitempty"`
	HostPath  string `json:"host_path,omitempty"`
}
