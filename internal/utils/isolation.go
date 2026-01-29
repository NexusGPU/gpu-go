package utils

import (
	tfv1 "github.com/NexusGPU/tensor-fusion/api/v1"
)

// IsolationMode constants matching tensor-fusion types
const (
	IsolationModeShared      = "shared"
	IsolationModeSoft        = "soft"
	IsolationModePartitioned = "partitioned"
)

// ToTFIsolationMode converts a string to tensor-fusion IsolationModeType
func ToTFIsolationMode(s string) tfv1.IsolationModeType {
	switch s {
	case IsolationModeSoft:
		return tfv1.IsolationModeSoft
	case IsolationModePartitioned:
		return tfv1.IsolationModePartitioned
	default:
		return tfv1.IsolationModeShared
	}
}

// FromTFIsolationMode converts tensor-fusion IsolationModeType to string
func FromTFIsolationMode(mode tfv1.IsolationModeType) string {
	switch mode {
	case tfv1.IsolationModeSoft:
		return IsolationModeSoft
	case tfv1.IsolationModePartitioned:
		return IsolationModePartitioned
	default:
		return IsolationModeShared
	}
}
