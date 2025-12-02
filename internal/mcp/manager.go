package mcp

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/statcode-ai/scriptschnell/internal/config"
	"github.com/statcode-ai/scriptschnell/internal/provider"
	"github.com/statcode-ai/scriptschnell/internal/tools"
)

// Manager converts MCP server definitions into executable tools.
type Manager struct {
	cfg         *config.Config
	workingDir  string
	httpClient  *http.Client
	providerMgr *provider.Manager
}

// NewManager creates a new MCP manager.
func NewManager(cfg *config.Config, workingDir string, providerMgr *provider.Manager) *Manager {
	return &Manager{
		cfg:         cfg,
		workingDir:  workingDir,
		providerMgr: providerMgr,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// BuildTools materializes configured MCP servers into tool implementations.
// Returns created tools and any errors that occurred while building individual servers.
func (m *Manager) BuildTools() ([]tools.Tool, []error) {
	if m == nil || m.cfg == nil {
		return nil, nil
	}

	var (
		result    []tools.Tool
		errs      []error
		nameUsage = make(map[string]int)
	)

	for serverName, serverCfg := range m.cfg.MCP.Servers {
		if serverCfg == nil {
			continue
		}
		if serverCfg.Disabled {
			continue
		}

		toolsForServer, err := m.buildServerTools(serverName, serverCfg, nameUsage)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", serverName, err))
			continue
		}
		result = append(result, toolsForServer...)
	}

	return result, errs
}

func (m *Manager) buildServerTools(serverName string, serverCfg *config.MCPServerConfig, nameUsage map[string]int) ([]tools.Tool, error) {
	if serverCfg == nil {
		return nil, fmt.Errorf("empty server configuration")
	}

	switch strings.ToLower(serverCfg.Type) {
	case "command":
		return m.buildCommandTools(serverName, serverCfg, nameUsage)
	case "openapi":
		return m.buildOpenAPITools(serverName, serverCfg, nameUsage)
	case "openai":
		return m.buildOpenAITools(serverName, serverCfg, nameUsage)
	default:
		return nil, fmt.Errorf("unsupported MCP server type: %s", serverCfg.Type)
	}
}

func (m *Manager) buildCommandTools(serverName string, serverCfg *config.MCPServerConfig, nameUsage map[string]int) ([]tools.Tool, error) {
	cmdCfg := serverCfg.Command
	if cmdCfg == nil {
		return nil, fmt.Errorf("command configuration missing")
	}
	if len(cmdCfg.Exec) == 0 {
		return nil, fmt.Errorf("command configuration requires at least one argument")
	}

	timeout := time.Duration(cmdCfg.TimeoutSeconds) * time.Second
	if cmdCfg.TimeoutSeconds == 0 {
		timeout = 60 * time.Second
	}

	name := uniqueToolName(fmt.Sprintf("mcp_%s", sanitizeName(serverName)), nameUsage)
	description := serverCfg.Description
	if description == "" {
		description = fmt.Sprintf("Execute %s via MCP command server", strings.Join(cmdCfg.Exec, " "))
	}

	tool := tools.NewCommandTool(&tools.CommandToolConfig{
		Name:        name,
		Description: description,
		Command:     cmdCfg.Exec,
		WorkingDir:  cmdCfg.WorkingDir,
		Env:         cmdCfg.Env,
		Timeout:     timeout,
	})

	return []tools.Tool{tool}, nil
}

func (m *Manager) buildOpenAITools(serverName string, serverCfg *config.MCPServerConfig, nameUsage map[string]int) ([]tools.Tool, error) {
	openAICfg := serverCfg.OpenAI
	if openAICfg == nil {
		return nil, fmt.Errorf("openai configuration missing")
	}

	name := uniqueToolName(fmt.Sprintf("mcp_%s", sanitizeName(serverName)), nameUsage)
	modelID := strings.TrimSpace(openAICfg.Model)
	if modelID == "" {
		if m.providerMgr != nil {
			modelID = strings.TrimSpace(m.providerMgr.GetOrchestrationModel())
		}
		if modelID == "" {
			return nil, fmt.Errorf("no model configured for OpenAI MCP server '%s'; please set an orchestration model or specify one explicitly", serverName)
		}
	}

	description := serverCfg.Description
	if description == "" {
		description = fmt.Sprintf("Call OpenAI model %s", modelID)
	}

	toolCfg := &tools.OpenAIToolConfig{
		Name:         name,
		Description:  description,
		Model:        modelID,
		APIKey:       openAICfg.APIKey,
		APIKeyEnv:    openAICfg.APIKeyEnvVar,
		BaseURL:      openAICfg.BaseURL,
		SystemPrompt: openAICfg.SystemPrompt,
		Temperature:  openAICfg.Temperature,
		MaxOutput:    openAICfg.MaxOutput,
		ResponseJSON: openAICfg.ResponseJSON,
	}

	tool := tools.NewOpenAITool(toolCfg)
	return []tools.Tool{tool}, nil
}

func (m *Manager) buildOpenAPITools(serverName string, serverCfg *config.MCPServerConfig, nameUsage map[string]int) ([]tools.Tool, error) {
	apiCfg := serverCfg.OpenAPI
	if apiCfg == nil {
		return nil, fmt.Errorf("openapi configuration missing")
	}

	specPath := strings.TrimSpace(apiCfg.SpecPath)
	if specPath == "" {
		return nil, fmt.Errorf("openapi spec_path is required")
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	var (
		doc *openapi3.T
		err error
	)

	if isURL(specPath) {
		doc, err = loader.LoadFromURI(parseURL(specPath))
	} else {
		resolved := specPath
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(m.workingDir, specPath)
		}
		doc, err = loader.LoadFromFile(resolved)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load OpenAPI spec: %w", err)
	}

	baseURL := strings.TrimSpace(apiCfg.URL)
	if baseURL == "" {
		return nil, fmt.Errorf("openapi server URL is required for MCP '%s'", serverName)
	}

	if doc.Paths == nil {
		return nil, fmt.Errorf("openapi spec contains no paths")
	}

	var toolsForServer []tools.Tool
	for path, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for method, operation := range pathItem.Operations() {
			if operation == nil {
				continue
			}

			parameters := collectParameters(pathItem.Parameters, operation.Parameters)
			requestBody := collectRequestBody(operation.RequestBody)

			toolNameBase := fmt.Sprintf("mcp_%s_%s", sanitizeName(serverName), sanitizeName(detectOperationName(operation, method, path)))
			toolName := uniqueToolName(toolNameBase, nameUsage)

			description := operation.Summary
			if description == "" {
				description = operation.Description
			}
			if description == "" {
				description = fmt.Sprintf("Call %s %s", strings.ToUpper(method), path)
			}

			headers := cloneStringMap(apiCfg.DefaultHeaders)
			if headers == nil {
				headers = make(map[string]string)
			}

			if _, exists := headers["Authorization"]; !exists {
				bearer := strings.TrimSpace(apiCfg.AuthBearerToken)
				if bearer == "" && apiCfg.AuthBearerEnv != "" {
					bearer = strings.TrimSpace(os.Getenv(apiCfg.AuthBearerEnv))
				}
				if bearer != "" {
					headers["Authorization"] = "Bearer " + bearer
				}
			}

			openapiTool := tools.NewOpenAPITool(&tools.OpenAPIToolConfig{
				Name:           toolName,
				Description:    description,
				BaseURL:        baseURL,
				Method:         method,
				Path:           path,
				Parameters:     parameters,
				RequestBody:    requestBody,
				DefaultHeaders: headers,
				DefaultQuery:   apiCfg.DefaultQuery,
				HTTPClient:     m.httpClient,
				Timeout:        60 * time.Second,
			})

			toolsForServer = append(toolsForServer, openapiTool)
		}
	}

	if len(toolsForServer) == 0 {
		return nil, fmt.Errorf("no operations discovered in OpenAPI spec")
	}

	return toolsForServer, nil
}

func collectParameters(pathParams openapi3.Parameters, opParams openapi3.Parameters) []*tools.OpenAPIParameter {
	all := make([]*tools.OpenAPIParameter, 0, len(pathParams)+len(opParams))
	seen := make(map[string]bool)

	appendParam := func(paramRef *openapi3.ParameterRef) {
		if paramRef == nil || paramRef.Value == nil {
			return
		}
		param := paramRef.Value
		key := fmt.Sprintf("%s:%s", param.In, param.Name)
		if seen[key] {
			return
		}
		seen[key] = true

		all = append(all, &tools.OpenAPIParameter{
			Name:        param.Name,
			In:          param.In,
			Key:         buildParameterKey(param),
			Required:    param.Required,
			Schema:      schemaRefToJSONSchema(param.Schema),
			Description: param.Description,
		})
	}

	for _, p := range pathParams {
		appendParam(p)
	}
	for _, p := range opParams {
		appendParam(p)
	}

	return all
}

func collectRequestBody(requestBodyRef *openapi3.RequestBodyRef) *tools.OpenAPIRequestBody {
	if requestBodyRef == nil || requestBodyRef.Value == nil {
		return nil
	}

	contentType := ""
	var schemaRef *openapi3.SchemaRef

	for ctype, media := range requestBodyRef.Value.Content {
		if schemaRef == nil {
			contentType = ctype
			schemaRef = media.Schema
		}
		if ctype == "application/json" {
			contentType = ctype
			schemaRef = media.Schema
			break
		}
	}

	return &tools.OpenAPIRequestBody{
		Required:    requestBodyRef.Value.Required,
		ContentType: contentType,
		Schema:      schemaRefToJSONSchema(schemaRef),
	}
}

func schemaRefToJSONSchema(schemaRef *openapi3.SchemaRef) map[string]interface{} {
	if schemaRef == nil || schemaRef.Value == nil {
		return nil
	}
	schema := schemaRef.Value

	result := map[string]interface{}{}
	if schema.Type != nil {
		types := schema.Type.Slice()
		if len(types) == 1 {
			result["type"] = types[0]
		} else if len(types) > 1 {
			result["type"] = types
		}
	}
	if schema.Format != "" {
		result["format"] = schema.Format
	}
	if schema.Description != "" {
		result["description"] = schema.Description
	}
	if len(schema.Enum) > 0 {
		result["enum"] = schema.Enum
	}
	if schema.Default != nil {
		result["default"] = schema.Default
	}
	if schema.Example != nil {
		result["example"] = schema.Example
	}
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}
	if schema.Items != nil {
		result["items"] = schemaRefToJSONSchema(schema.Items)
	}
	if schema.Properties != nil {
		props := make(map[string]interface{}, len(schema.Properties))
		for key, propRef := range schema.Properties {
			props[key] = schemaRefToJSONSchema(propRef)
		}
		result["properties"] = props
	}
	if schema.AdditionalProperties.Schema != nil {
		result["additionalProperties"] = schemaRefToJSONSchema(schema.AdditionalProperties.Schema)
	} else if schema.AdditionalProperties.Has != nil {
		result["additionalPropertiesAllowed"] = *schema.AdditionalProperties.Has
	}
	if schema.AnyOf != nil {
		result["anyOf"] = schemaRefsToSlice(schema.AnyOf)
	}
	if schema.AllOf != nil {
		result["allOf"] = schemaRefsToSlice(schema.AllOf)
	}
	if schema.OneOf != nil {
		result["oneOf"] = schemaRefsToSlice(schema.OneOf)
	}

	return result
}

func schemaRefsToSlice(refs openapi3.SchemaRefs) []interface{} {
	out := make([]interface{}, 0, len(refs))
	for _, ref := range refs {
		out = append(out, schemaRefToJSONSchema(ref))
	}
	return out
}

func buildParameterKey(param *openapi3.Parameter) string {
	if param == nil {
		return ""
	}
	parts := []string{param.In, param.Name}
	return sanitizeName(strings.Join(parts, "_"))
}

func sanitizeName(name string) string {
	if name == "" {
		return "mcp"
	}
	name = strings.ToLower(name)
	var b strings.Builder
	prevUnderscore := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	result := strings.Trim(b.String(), "_")
	if result == "" {
		return "mcp"
	}
	return result
}

func uniqueToolName(base string, usage map[string]int) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "mcp_tool"
	}
	count := usage[base]
	if count == 0 {
		usage[base] = 1
		return base
	}
	count++
	usage[base] = count
	return fmt.Sprintf("%s_%d", base, count)
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func detectOperationName(operation *openapi3.Operation, method, path string) string {
	if operation != nil && operation.OperationID != "" {
		return operation.OperationID
	}
	return fmt.Sprintf("%s_%s", method, strings.Trim(path, "/"))
}

func isURL(path string) bool {
	parsed, err := url.Parse(path)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

func parseURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		return &url.URL{}
	}
	return parsed
}
