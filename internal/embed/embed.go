// Package embed manages the embedding model for semantic search.
// Downloads nomic-embed-text v1.5 from HuggingFace on first use,
// then generates 768-dimensional embeddings using Hugot's pure Go backend.
package embed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/jacobchapa/gchat/internal/config"
	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

const (
	modelName       = "nomic-ai/nomic-embed-text-v1.5"
	modelDirName    = "nomic-ai_nomic-embed-text-v1.5"
	onnxFilename    = "onnx/model_quantized.onnx"
	onnxLocalFile   = "model_quantized.onnx"
	dimensions      = 768
)

// Embedder generates text embeddings using the nomic-embed-text v1.5 model.
type Embedder struct {
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
	mu       sync.Mutex
}

// New creates a new Embedder. Downloads the model if needed, then loads it.
func New(ctx context.Context) (*Embedder, error) {
	modelPath, err := ensureModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("embed: model setup failed: %w", err)
	}

	session, err := hugot.NewGoSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("embed: cannot create session: %w", err)
	}

	cfg := hugot.FeatureExtractionConfig{
		ModelPath:    modelPath,
		Name:         "gchat-embedder",
		OnnxFilename: onnxLocalFile,
	}

	pipe, err := hugot.NewPipeline(session, cfg)
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("embed: cannot create pipeline: %w", err)
	}

	return &Embedder{session: session, pipeline: pipe}, nil
}

// Embed generates an embedding for a single text string.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("embed: no embedding returned")
	}
	return results[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
// Texts are processed in a single batch for efficiency.
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	result, err := e.pipeline.RunPipeline(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed: pipeline failed: %w", err)
	}

	return result.Embeddings, nil
}

// Dimensions returns the embedding vector size.
func (e *Embedder) Dimensions() int {
	return dimensions
}

// Close releases model resources.
func (e *Embedder) Close() error {
	if e.session != nil {
		return e.session.Destroy()
	}
	return nil
}

// ensureModel downloads the model if it doesn't exist locally.
func ensureModel(ctx context.Context) (string, error) {
	modelsDir, err := config.ModelsDir()
	if err != nil {
		return "", err
	}

	modelPath := filepath.Join(modelsDir, modelDirName)
	onnxPath := filepath.Join(modelPath, onnxLocalFile)

	if _, err := os.Stat(onnxPath); err == nil {
		return modelPath, nil
	}

	fmt.Fprintf(os.Stderr, "Downloading %s...\n", modelName)

	opts := hugot.NewDownloadOptions()
	opts.OnnxFilePath = onnxFilename
	opts.Verbose = true

	downloaded, err := hugot.DownloadModel(ctx, modelName, modelsDir, opts)
	if err != nil {
		return "", fmt.Errorf("embed: download failed: %w", err)
	}

	return downloaded, nil
}

// ModelExists returns true if the embedding model is already downloaded.
func ModelExists() bool {
	modelsDir, err := config.ModelsDir()
	if err != nil {
		return false
	}
	onnxPath := filepath.Join(modelsDir, modelDirName, onnxLocalFile)
	_, err = os.Stat(onnxPath)
	return err == nil
}
