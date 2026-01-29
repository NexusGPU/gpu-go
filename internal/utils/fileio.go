// Package utils provides shared utilities for GPU Go.
package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to a file atomically by writing to a temp file first
// and then renaming. This prevents partial writes on failure.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

// LoadJSON loads JSON from a file into the provided pointer.
// Returns nil, nil if file doesn't exist (not an error).
func LoadJSON[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SaveJSON saves data as JSON to the specified path atomically.
func SaveJSON[T any](path string, data T, perm os.FileMode) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, jsonData, perm)
}

// LoadJSONSlice loads a JSON array from a file into a slice.
// Returns nil, nil if file doesn't exist (not an error).
func LoadJSONSlice[T any](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var result []T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// SaveJSONSlice saves a slice as JSON to the specified path atomically.
func SaveJSONSlice[T any](path string, data []T, perm os.FileMode) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, jsonData, perm)
}
