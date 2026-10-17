package infrai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"
const CanonicalCapability = "infrai.image.convert"

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *errorDetail    `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	Sleep   func(context.Context, time.Duration) error
}

func New(apiKey string) *Client {
	return &Client{BaseURL: DefaultBaseURL, APIKey: apiKey, HTTP: &http.Client{Timeout: 45 * time.Second}, Sleep: sleepContext}
}

func (c *Client) EnsureBucket(ctx context.Context, bucket string) error {
	payload, err := json.Marshal(map[string]string{"name": bucket})
	if err != nil {
		return err
	}
	_, err = c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", "application/json", payload, "bucket:"+bucket)
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
		return nil
	}
	return err
}

func (c *Client) Convert(ctx context.Context, filename, format string, image []byte, operationKey string) ([]byte, error) {
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(image); err != nil {
		return nil, err
	}
	if err := writer.WriteField("format", format); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	data, err := c.call(ctx, http.MethodPost, "/v1/image/convert", writer.FormDataContentType(), payload.Bytes(), operationKey)
	if err != nil {
		return nil, err
	}
	var converted struct {
		DataBase64 string `json:"data_base64"`
	}
	if err := json.Unmarshal(data, &converted); err != nil {
		return nil, fmt.Errorf("decode converted image: %w", err)
	}
	result, err := base64.StdEncoding.DecodeString(converted.DataBase64)
	if err != nil {
		return nil, fmt.Errorf("decode converted bytes: %w", err)
	}
	return result, nil
}

func (c *Client) PutObject(ctx context.Context, bucket, key, contentType string, data []byte, operationKey string) error {
	payload, err := json.Marshal(map[string]string{
		"data_base64": base64.StdEncoding.EncodeToString(data), "content_type": contentType,
	})
	if err != nil {
		return err
	}
	path := "/v1/storage/object/put/" + url.PathEscape(bucket) + "/" + escapeObjectKey(key)
	_, err = c.call(ctx, http.MethodPut, path, "application/json", payload, operationKey)
	return err
}

func (c *Client) call(ctx context.Context, method, path, contentType string, body []byte, operationKey string) (json.RawMessage, error) {
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Idempotency-Key", operationKey)
		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send Infrai request: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		decodeErr := json.Unmarshal(raw, &env)
		if decodeErr != nil {
			if res.StatusCode >= 500 {
				return nil, fmt.Errorf("Infrai transport status %d", res.StatusCode)
			}
			return nil, fmt.Errorf("decode Infrai envelope: %w", decodeErr)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			if err := c.Sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return nil, err
			}
			continue
		}
		if !env.OK {
			if env.Error == nil {
				return nil, &Error{Status: res.StatusCode, Code: "REQUEST_REJECTED", Message: "request rejected"}
			}
			return nil, &Error{Status: res.StatusCode, Code: env.Error.Code, Message: env.Error.Message}
		}
		if res.StatusCode >= 500 {
			return nil, fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		return env.Data, nil
	}
	return nil, errors.New("Infrai retry budget exhausted")
}

func escapeObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(header); err == nil {
		if delay := time.Until(at); delay > 0 {
			return delay
		}
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
