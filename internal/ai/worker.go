package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultWorkerSocket = "/run/meeple-ai/worker.sock"
const maxWorkerPayload = 1 << 20

func workerSocketPath() string {
	if path := strings.TrimSpace(os.Getenv("AI_WORKER_SOCKET")); path != "" {
		return path
	}
	return defaultWorkerSocket
}

func (c *Client) completeWorker(ctx context.Context, payload []byte) (response, error) {
	var result response
	socket := c.workerSocket
	if socket == "" {
		socket = workerSocketPath()
	}
	httpClient := &http.Client{Timeout: 100 * time.Second, Transport: &http.Transport{DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://worker/complete", bytes.NewReader(payload))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return result, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return result, fmt.Errorf("ai worker returned HTTP %d", res.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result)
	return result, err
}

// ServeWorker accepts only local Unix-socket requests. The worker's OS user has
// the model credential but does not inherit the API process's database secrets.
func ServeWorker(ctx context.Context, socket, cliPath string) error {
	if socket == "" {
		socket = workerSocketPath()
	}
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close(); _ = os.Remove(socket) }()
	if err := os.Chmod(socket, 0660); err != nil {
		return err
	}
	cli := &Client{provider: "codex-cli", cliPath: cliPath}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 100 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/complete" {
				http.NotFound(w, r)
				return
			}
			// Rule answers send a whole rulebook (up to 120k runes plus JSON escaping).
			payload, err := io.ReadAll(io.LimitReader(r.Body, maxWorkerPayload+1))
			if err != nil || len(payload) > maxWorkerPayload {
				http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
				return
			}
			result, err := cli.completeCLI(r.Context(), payload)
			if err != nil {
				slog.Warn("Codex CLI unavailable", "error", err)
				http.Error(w, "AI worker unavailable", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
		}),
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
