package utils

import (
	"os"
	"path/filepath"
	"testing"

	tfv1 "github.com/NexusGPU/tensor-fusion/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestAtomicWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	data := []byte("hello world")

	err := AtomicWriteFile(path, data, 0644)
	require.NoError(t, err)

	readData, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data, readData)

	// Check permissions
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtimeOS := os.Getenv("GOOS"); runtimeOS != "windows" {
		assert.Equal(t, os.FileMode(0644), info.Mode().Perm())
	}
}

func TestJSONHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "data.json")

	// SaveJSON
	obj := &testStruct{Name: "test", Value: 123}
	err := SaveJSON(path, obj, 0644)
	require.NoError(t, err)

	// LoadJSON
	loaded, err := LoadJSON[testStruct](path)
	require.NoError(t, err)
	assert.Equal(t, obj, loaded)

	// LoadJSON not exist
	loaded, err = LoadJSON[testStruct](filepath.Join(tmpDir, "missing.json"))
	require.NoError(t, err)
	assert.Nil(t, loaded)

	// SaveJSONSlice
	slicePath := filepath.Join(tmpDir, "slice.json")
	slice := []testStruct{
		{Name: "one", Value: 1},
		{Name: "two", Value: 2},
	}
	err = SaveJSONSlice(slicePath, slice, 0644)
	require.NoError(t, err)

	// LoadJSONSlice
	loadedSlice, err := LoadJSONSlice[testStruct](slicePath)
	require.NoError(t, err)
	assert.Equal(t, slice, loadedSlice)

	// LoadJSONSlice not exist
	loadedSlice, err = LoadJSONSlice[testStruct](filepath.Join(tmpDir, "missing_slice.json"))
	require.NoError(t, err)
	assert.Nil(t, loadedSlice)
}

func TestIsolationModeConversion(t *testing.T) {
	tests := []struct {
		input    string
		expected tfv1.IsolationModeType
	}{
		{"soft", tfv1.IsolationModeSoft},
		{"partitioned", tfv1.IsolationModePartitioned},
		{"shared", tfv1.IsolationModeShared},
		{"unknown", tfv1.IsolationModeShared},
		{"", tfv1.IsolationModeShared},
	}

	for _, tt := range tests {
		result := ToTFIsolationMode(tt.input)
		assert.Equal(t, tt.expected, result, "ToTFIsolationMode(%s)", tt.input)
	}

	reverseTests := []struct {
		input    tfv1.IsolationModeType
		expected string
	}{
		{tfv1.IsolationModeSoft, "soft"},
		{tfv1.IsolationModePartitioned, "partitioned"},
		{tfv1.IsolationModeShared, "shared"},
	}

	for _, tt := range reverseTests {
		result := FromTFIsolationMode(tt.input)
		assert.Equal(t, tt.expected, result, "FromTFIsolationMode(%s)", tt.input)
	}
}
