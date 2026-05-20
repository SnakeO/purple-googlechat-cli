// Package embed tests model existence checks and embedding generation.
// Full embedding tests require the model to be downloaded (~274MB)
// and are gated behind the -integration flag.
package embed

import (
	"os"
	"path/filepath"
	"testing"
)

// TestModelExistsWhenMissing verifies false when no model is present.
func TestModelExistsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if ModelExists() {
		t.Error("ModelExists should return false when model is missing")
	}
}

// TestModelExistsWhenPresent verifies true when model file exists.
func TestModelExistsWhenPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	modelDir := filepath.Join(tmp, ".config", "gchat", "models", modelDirName)
	os.MkdirAll(modelDir, 0700)
	os.WriteFile(filepath.Join(modelDir, onnxLocalFile), []byte("fake"), 0600)

	if !ModelExists() {
		t.Error("ModelExists should return true when model file exists")
	}
}

// TestDimensions verifies the embedding dimensions constant.
func TestDimensions(t *testing.T) {
	if dimensions != 768 {
		t.Errorf("expected 768 dimensions, got %d", dimensions)
	}
}

// TestModelConstants verifies model configuration constants.
func TestModelConstants(t *testing.T) {
	if modelName != "nomic-ai/nomic-embed-text-v1.5" {
		t.Errorf("unexpected model name: %s", modelName)
	}
	if onnxFilename != "onnx/model_quantized.onnx" {
		t.Errorf("unexpected onnx filename: %s", onnxFilename)
	}
}
