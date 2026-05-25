package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// NewOpenAIEmbedder creates an EmbedFunc that calls OpenAI's embedding API.
func NewOpenAIEmbedder(apiKey string, model string) EmbedFunc {
	if model == "" {
		model = "text-embedding-ada-002"
	}
	client := &http.Client{Timeout: 30 * time.Second}
	return func(text string) ([]float32, error) {
		body, _ := json.Marshal(map[string]string{
			"input": text,
			"model": model,
		})
		req, err := http.NewRequest("POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := doWithRetry(client, req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var result struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
			Error *struct{ Message string } `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode embedding response: %w", err)
		}
		if result.Error != nil {
			return nil, fmt.Errorf("openai error: %s", result.Error.Message)
		}
		if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
			return nil, fmt.Errorf("no embedding in response")
		}
		return result.Data[0].Embedding, nil
	}
}

// NewBatchEmbedder creates a function that embeds multiple texts in one API call.
func NewBatchEmbedder(apiKey string, model string) func(texts []string) ([][]float32, error) {
	if model == "" {
		model = "text-embedding-ada-002"
	}
	client := &http.Client{Timeout: 60 * time.Second}
	return func(texts []string) ([][]float32, error) {
		body, _ := json.Marshal(map[string]interface{}{
			"input": texts,
			"model": model,
		})
		req, err := http.NewRequest("POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := doWithRetry(client, req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var result struct {
			Data []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
			Error *struct{ Message string } `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode batch embedding response: %w", err)
		}
		if result.Error != nil {
			return nil, fmt.Errorf("openai error: %s", result.Error.Message)
		}

		out := make([][]float32, len(texts))
		for _, d := range result.Data {
			if d.Index < len(out) {
				out[d.Index] = d.Embedding
			}
		}
		return out, nil
	}
}

func doWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		time.Sleep(1 * time.Second)
		resp, err = client.Do(req)
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d from embedding API", resp.StatusCode)
	}
	return resp, nil
}
