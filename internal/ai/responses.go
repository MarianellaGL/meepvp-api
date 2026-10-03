package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

func (c *Client) complete(ctx context.Context, payload []byte) (response, error) {
	return c.completeResponses(ctx, payload)
}

func (c *Client) completeResponses(ctx context.Context, payload []byte) (response, error) {
	var result response
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return result, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		var failure struct {
			Error struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&failure)
		retryAfter, _ := strconv.Atoi(res.Header.Get("Retry-After"))
		return result, &ProviderError{StatusCode: res.StatusCode, Code: failure.Error.Code, Type: failure.Error.Type, RetryAfterSeconds: max(0, retryAfter)}
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result)
	return result, err
}

type ProviderError struct {
	StatusCode        int
	Code              string
	Type              string
	RetryAfterSeconds int
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("OpenAI returned HTTP %d (code=%s, type=%s)", e.StatusCode, e.Code, e.Type)
}

func (e *ProviderError) QuotaExhausted() bool {
	switch e.Code {
	case "credit_balance_exhausted", "organization_spend_limit_exceeded", "project_spend_limit_exceeded", "organization_usage_limit_exceeded", "insufficient_quota":
		return true
	}
	return e.Type == "insufficient_quota"
}
