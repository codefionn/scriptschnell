package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// OpenAPIParameter describes an OpenAPI parameter exposed to the tool schema.
type OpenAPIParameter struct {
	Name        string
	In          string
	Key         string
	Required    bool
	Schema      map[string]interface{}
	Description string
}

// OpenAPIRequestBody captures body metadata for the tool execution.
type OpenAPIRequestBody struct {
	Required    bool
	ContentType string
	Schema      map[string]interface{}
}

// OpenAPIToolConfig contains the information required to build an OpenAPITool.
type OpenAPIToolConfig struct {
	Name           string
	Description    string
	BaseURL        string
	Method         string
	Path           string
	Parameters     []*OpenAPIParameter
	RequestBody    *OpenAPIRequestBody
	DefaultHeaders map[string]string
	DefaultQuery   map[string]string
	HTTPClient     *http.Client
	Timeout        time.Duration
}

// OpenAPITool executes HTTP operations defined by an OpenAPI spec.
type OpenAPITool struct {
	name           string
	description    string
	method         string
	baseURL        string
	path           string
	parameters     []*OpenAPIParameter
	requestBody    *OpenAPIRequestBody
	defaultHeaders map[string]string
	defaultQuery   map[string]string
	client         *http.Client
	timeout        time.Duration
}

// NewOpenAPITool constructs a new OpenAPITool from config.
func NewOpenAPITool(cfg *OpenAPIToolConfig) *OpenAPITool {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	return &OpenAPITool{
		name:           cfg.Name,
		description:    cfg.Description,
		method:         strings.ToUpper(cfg.Method),
		baseURL:        cfg.BaseURL,
		path:           cfg.Path,
		parameters:     cfg.Parameters,
		requestBody:    cfg.RequestBody,
		defaultHeaders: cfg.DefaultHeaders,
		defaultQuery:   cfg.DefaultQuery,
		client:         client,
		timeout:        timeout,
	}
}

// Name implements Tool.
func (o *OpenAPITool) Name() string {
	return o.name
}

// Description implements Tool.
func (o *OpenAPITool) Description() string {
	if o.description != "" {
		return o.description
	}
	return fmt.Sprintf("Invoke %s %s", o.method, o.path)
}

// Parameters implements Tool.
func (o *OpenAPITool) Parameters() map[string]interface{} {
	properties := make(map[string]interface{})
	required := make([]string, 0)

	for _, p := range o.parameters {
		if p == nil {
			continue
		}

		schema := cloneJSON(p.Schema)
		if schema == nil {
			schema = map[string]interface{}{"type": "string"}
		}
		if p.Description != "" {
			schema["description"] = strings.TrimSpace(p.Description)
		}
		schema["in"] = p.In

		properties[p.Key] = schema
		if p.Required {
			required = append(required, p.Key)
		}
	}

	if o.requestBody != nil {
		bodySchema := cloneJSON(o.requestBody.Schema)
		if bodySchema == nil {
			bodySchema = map[string]interface{}{"type": "object"}
		}
		if o.requestBody.ContentType != "" {
			bodySchema["content_type"] = o.requestBody.ContentType
		}
		bodySchema["description"] = "HTTP request body payload"
		properties["body"] = bodySchema
		if o.requestBody.Required {
			required = append(required, "body")
		}
	}

	result := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		result["required"] = required
	}

	return result
}

// Execute performs the HTTP request defined by the tool configuration.
func (o *OpenAPITool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	reqURL, err := o.buildURL(params)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	contentType := ""
	if o.requestBody != nil {
		var bodyValue interface{}
		if raw, ok := params["body"]; ok {
			bodyValue = raw
		}
		if bodyValue == nil {
			if o.requestBody.Required {
				return nil, fmt.Errorf("body is required")
			}
		} else {
			switch v := bodyValue.(type) {
			case string:
				bodyReader = strings.NewReader(v)
			default:
				data, marshalErr := json.Marshal(v)
				if marshalErr != nil {
					return nil, fmt.Errorf("failed to marshal body: %w", marshalErr)
				}
				bodyReader = bytes.NewReader(data)
				if o.requestBody.ContentType == "" {
					contentType = "application/json"
				}
			}
		}
		if contentType == "" {
			contentType = o.requestBody.ContentType
		}
	}

	req, err := http.NewRequestWithContext(ctx, o.method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Apply default headers first.
	for k, v := range o.defaultHeaders {
		req.Header.Set(k, v)
	}

	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Populate header parameters from user input.
	for _, p := range o.parameters {
		if p == nil || p.In != "header" {
			continue
		}
		value, ok := params[p.Key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case []interface{}:
			for _, item := range v {
				req.Header.Add(p.Name, fmt.Sprint(item))
			}
		case []string:
			for _, item := range v {
				req.Header.Add(p.Name, item)
			}
		default:
			req.Header.Set(p.Name, fmt.Sprint(v))
		}
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	result := map[string]interface{}{
		"url":        req.URL.String(),
		"method":     o.method,
		"status":     resp.StatusCode,
		"statusText": resp.Status,
		"headers":    resp.Header,
	}

	var parsed interface{}
	if len(respBody) > 0 {
		if json.Unmarshal(respBody, &parsed) == nil {
			result["body"] = parsed
		} else {
			result["body"] = string(respBody)
		}
	} else {
		result["body"] = ""
	}

	return result, nil
}

func (o *OpenAPITool) buildURL(params map[string]interface{}) (string, error) {
	baseURL := strings.TrimSpace(o.baseURL)
	relativePath := o.path

	// Substitute path parameters.
	for _, p := range o.parameters {
		if p == nil || p.In != "path" {
			continue
		}
		value, ok := params[p.Key]
		if !ok {
			if p.Required {
				return "", fmt.Errorf("missing required path parameter: %s", p.Key)
			}
			continue
		}
		replacement := url.PathEscape(fmt.Sprint(value))
		relativePath = strings.ReplaceAll(relativePath, "{"+p.Name+"}", replacement)
	}

	var fullURL string
	if baseURL == "" {
		fullURL = relativePath
	} else {
		base, err := url.Parse(baseURL)
		if err != nil {
			return "", fmt.Errorf("invalid base URL %q: %w", baseURL, err)
		}
		if !strings.HasPrefix(relativePath, "/") {
			relativePath = "/" + relativePath
		}
		base.Path = path.Join(strings.TrimSuffix(base.Path, "/"), relativePath)
		fullURL = base.String()
	}

	parsedURL, err := url.Parse(fullURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %q: %w", fullURL, err)
	}

	queryValues := parsedURL.Query()
	for key, val := range o.defaultQuery {
		queryValues.Set(key, val)
	}

	for _, p := range o.parameters {
		if p == nil || p.In != "query" {
			continue
		}
		value, ok := params[p.Key]
		if !ok {
			if p.Required {
				return "", fmt.Errorf("missing required query parameter: %s", p.Key)
			}
			continue
		}
		switch v := value.(type) {
		case []interface{}:
			for _, item := range v {
				queryValues.Add(p.Name, fmt.Sprint(item))
			}
		case []string:
			for _, item := range v {
				queryValues.Add(p.Name, item)
			}
		default:
			queryValues.Set(p.Name, fmt.Sprint(v))
		}
	}

	parsedURL.RawQuery = queryValues.Encode()
	return parsedURL.String(), nil
}

func cloneJSON(val map[string]interface{}) map[string]interface{} {
	if val == nil {
		return nil
	}
	out := make(map[string]interface{}, len(val))
	for k, v := range val {
		out[k] = v
	}
	return out
}
