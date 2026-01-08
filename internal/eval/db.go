package eval

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Database handles SQLite operations for the eval system
type Database struct {
	db     *sql.DB
	dbPath string
}

// NewDatabase creates a new database connection
func NewDatabase(dbPath string) (*Database, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	database := &Database{db: db, dbPath: dbPath}

	// Initialize schema
	if err := database.migrate(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return database, nil
}

// getConfigDir returns the directory containing the database
func (d *Database) getConfigDir() string {
	return filepath.Dir(d.dbPath)
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// migrate ensures the database schema is up to date
func (d *Database) migrate() error {
	// Step 1: Create initial tables if they don't exist
	// This includes primary keys, foreign keys and indexes which are
	// easier to define via SQL than reflection without custom tags.
	schema := `
	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS eval_models (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		provider TEXT NOT NULL,
		selected BOOLEAN NOT NULL DEFAULT FALSE,
		description TEXT,
		context_window INTEGER
	);

	CREATE TABLE IF NOT EXISTS eval_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		eval_id TEXT NOT NULL,
		model_id TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		agent_output TEXT,
		agent_exit_code INTEGER,
		started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		completed_at DATETIME,
		FOREIGN KEY (model_id) REFERENCES eval_models(id)
	);

	CREATE TABLE IF NOT EXISTS eval_results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id INTEGER NOT NULL,
		test_case_id TEXT NOT NULL,
		passed BOOLEAN NOT NULL DEFAULT FALSE,
		actual_output TEXT,
		expected_output TEXT,
		error TEXT,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		estimate_cost REAL DEFAULT 0.0,
		response_time INTEGER DEFAULT 0,
		execution_time INTEGER DEFAULT 0,
		container_name TEXT,
		raw_output TEXT,
		errors TEXT,
		detailed_execution_info TEXT,
		FOREIGN KEY (run_id) REFERENCES eval_runs(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_eval_runs_eval_model ON eval_runs(eval_id, model_id);
	CREATE INDEX IF NOT EXISTS idx_eval_results_run_id ON eval_results(run_id);
	`

	_, err := d.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create initial schema: %w", err)
	}

	// Step 2: Auto-migrate by detecting new fields in structs
	if err := d.autoMigrateTable("eval_models", &EvalModel{}); err != nil {
		return fmt.Errorf("failed to auto-migrate eval_models: %w", err)
	}
	if err := d.autoMigrateTable("eval_runs", &EvalRun{}); err != nil {
		return fmt.Errorf("failed to auto-migrate eval_runs: %w", err)
	}
	if err := d.autoMigrateTable("eval_results", &EvalResult{}); err != nil {
		return fmt.Errorf("failed to auto-migrate eval_results: %w", err)
	}

	// Add sample models if the database is empty
	return d.addSampleModels()
}

// autoMigrateTable adds missing columns to a table based on struct tags
func (d *Database) autoMigrateTable(tableName string, model interface{}) error {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Get existing columns
	existingColumns := make(map[string]bool)
	rows, err := d.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var dtype string
		var notnull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &dtype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		existingColumns[strings.ToLower(name)] = true
	}

	// Iterate through struct fields
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		dbTag := field.Tag.Get("db")
		if dbTag == "" || dbTag == "-" {
			continue
		}

		// Use the first part of the tag (before comma) as column name
		columnName := strings.Split(dbTag, ",")[0]

		// If column doesn't exist, add it
		if !existingColumns[strings.ToLower(columnName)] {
			sqlType := d.getSQLiteType(field.Type)
			query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, sqlType)
			if _, err := d.db.Exec(query); err != nil {
				return fmt.Errorf("failed to add column %s: %w", columnName, err)
			}
		}
	}

	return nil
}

// getSQLiteType returns the appropriate SQLite type for a Go type
func (d *Database) getSQLiteType(t reflect.Type) string {
	// Handle pointers
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return "TEXT"
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		return "INTEGER"
	case reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8:
		return "INTEGER"
	case reflect.Bool:
		return "BOOLEAN"
	case reflect.Float64, reflect.Float32:
		return "REAL"
	default:
		// Check for time.Time
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return "DATETIME"
		}
		return "TEXT"
	}
}

// initSchema is kept for backward compatibility if needed, but now calls migrate

// addSampleModels adds some sample models to demonstrate the functionality
func (d *Database) addSampleModels() error {
	// Check if models already exist
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM eval_models").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil // Models already exist, don't add samples
	}

	// Sample models for demonstration
	sampleModels := []*EvalModel{
		{
			ID:            "openai/gpt-4o",
			Name:          "GPT-4o",
			Provider:      "OpenAI",
			Description:   "GPT-4o is a large language model that can engage in natural language conversations, answer questions, and assist with tasks.",
			ContextWindow: 128000,
		},
		{
			ID:            "openai/gpt-4o-mini",
			Name:          "GPT-4o Mini",
			Provider:      "OpenAI",
			Description:   "GPT-4o Mini is a smaller, more efficient version of GPT-4o with excellent performance for many tasks.",
			ContextWindow: 128000,
		},
		{
			ID:            "anthropic/claude-3.5-sonnet",
			Name:          "Claude 3.5 Sonnet",
			Provider:      "Anthropic",
			Description:   "Claude 3.5 Sonnet is a highly capable AI assistant with strong reasoning and conversational abilities.",
			ContextWindow: 200000,
		},
		{
			ID:            "anthropic/claude-3.5-haiku",
			Name:          "Claude 3.5 Haiku",
			Provider:      "Anthropic",
			Description:   "Claude 3.5 Haiku is a fast, efficient model for quick tasks and responses.",
			ContextWindow: 200000,
		},
		{
			ID:            "google/gemini-1.5-pro",
			Name:          "Gemini 1.5 Pro",
			Provider:      "Google",
			Description:   "Gemini 1.5 Pro is Google's advanced multimodal AI model with broad capabilities.",
			ContextWindow: 2000000,
		},
		{
			ID:            "google/gemini-1.5-flash",
			Name:          "Gemini 1.5 Flash",
			Provider:      "Google",
			Description:   "Gemini 1.5 Flash is a fast, efficient model for quick responses and tasks.",
			ContextWindow: 1000000,
		},
		{
			ID:            "meta-llama/llama-3.1-405b-instruct",
			Name:          "Llama 3.1 405B Instruct",
			Provider:      "Meta",
			Description:   "Llama 3.1 405B is Meta's largest open model with excellent instruction following.",
			ContextWindow: 131072,
		},
		{
			ID:            "google/gemma-2-27b-it",
			Name:          "Gemma 2 27B IT",
			Provider:      "Google",
			Description:   "Gemma 2 27B is an instruction-tuned model optimized for conversational tasks.",
			ContextWindow: 8192,
		},
		{
			ID:            "microsoft/wizardlm-2-8x22b",
			Name:          "WizardLM-2 8x22B",
			Provider:      "Microsoft",
			Description:   "WizardLM-2 is a model trained for complex instruction following and code generation.",
			ContextWindow: 65536,
		},
		{
			ID:            "qwen/qwen-2.5-72b-instruct",
			Name:          "Qwen 2.5 72B Instruct",
			Provider:      "Qwen",
			Description:   "Qwen 2.5 72B is a large multilingual model optimized for instruction following.",
			ContextWindow: 131072,
		},
		{
			ID:            "deepseek/deepseek-coder",
			Name:          "DeepSeek Coder",
			Provider:      "DeepSeek",
			Description:   "DeepSeek Coder is specialized for code generation and programming tasks.",
			ContextWindow: 65536,
		},
		{
			ID:            "thudm/glm-4-flash",
			Name:          "GLM-4-Flash",
			Provider:      "THUDM",
			Description:   "GLM-4-Flash is a fast chatbot model optimized for conversational interactions.",
			ContextWindow: 128000,
		},
	}

	// Insert sample models
	for _, model := range sampleModels {
		if err := d.AddModel(model); err != nil {
			return fmt.Errorf("failed to add sample model %s: %w", model.ID, err)
		}
	}

	return nil
}

// Config operations

// GetConfig gets a configuration value
func (d *Database) GetConfig(key string) (string, error) {
	var value string
	err := d.db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetConfig sets a configuration value
func (d *Database) SetConfig(key, value string) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO config (key, value) VALUES (?, ?)
	`, key, value)
	return err
}

// GetEvalConfig gets the eval configuration
func (d *Database) GetEvalConfig() (*EvalConfig, error) {
	config := &EvalConfig{}

	token, err := d.GetConfig("openrouter_token")
	if err != nil {
		return nil, err
	}
	config.OpenRouterToken = token

	return config, nil
}

// SetEvalConfig sets the eval configuration
func (d *Database) SetEvalConfig(config *EvalConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	return d.SetConfig("openrouter_token", config.OpenRouterToken)
}

// Model operations

// AddModel adds a model to the database
func (d *Database) AddModel(model *EvalModel) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO eval_models (id, name, provider, selected, description, context_window)
		VALUES (?, ?, ?, ?, ?, ?)
	`, model.ID, model.Name, model.Provider, model.Selected, model.Description, model.ContextWindow)
	return err
}

// GetModels gets all models from the database
func (d *Database) GetModels() ([]*EvalModel, error) {
	rows, err := d.db.Query(`
		SELECT id, name, provider, selected, description, context_window
		FROM eval_models
		ORDER BY provider, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []*EvalModel
	for rows.Next() {
		model := &EvalModel{}
		err := rows.Scan(&model.ID, &model.Name, &model.Provider, &model.Selected, &model.Description, &model.ContextWindow)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}

	return models, nil
}

// GetSelectedModels gets all selected models
func (d *Database) GetSelectedModels() ([]*EvalModel, error) {
	rows, err := d.db.Query(`
		SELECT id, name, provider, selected, description, context_window
		FROM eval_models
		WHERE selected = TRUE
		ORDER BY provider, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []*EvalModel
	for rows.Next() {
		model := &EvalModel{}
		err := rows.Scan(&model.ID, &model.Name, &model.Provider, &model.Selected, &model.Description, &model.ContextWindow)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}

	return models, nil
}

// UpdateModelSelection updates the selection status of a model
func (d *Database) UpdateModelSelection(modelID string, selected bool) error {
	_, err := d.db.Exec(`
		UPDATE eval_models SET selected = ? WHERE id = ?
	`, selected, modelID)
	return err
}

// ClearModelSelection clears all model selections
func (d *Database) ClearModelSelection() error {
	_, err := d.db.Exec(`
		UPDATE eval_models SET selected = FALSE
	`)
	return err
}

// DeleteModel removes a model from the database
func (d *Database) DeleteModel(modelID string) error {
	_, err := d.db.Exec(`
		DELETE FROM eval_models WHERE id = ?
	`, modelID)
	return err
}

// Eval run operations

// CreateEvalRun creates a new eval run
func (d *Database) CreateEvalRun(evalID, modelID string) (*EvalRun, error) {
	now := time.Now()

	result, err := d.db.Exec(`
		INSERT INTO eval_runs (eval_id, model_id, status, started_at)
		VALUES (?, ?, 'pending', ?)
	`, evalID, modelID, now)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	run := &EvalRun{
		ID:        id,
		EvalID:    evalID,
		ModelID:   modelID,
		Status:    "pending",
		StartedAt: now,
	}

	return run, nil
}

// GetEvalRun gets an eval run by ID
func (d *Database) GetEvalRun(id int64) (*EvalRun, error) {
	var completedAt *time.Time
	var agentOutput sql.NullString
	run := &EvalRun{ID: id}

	err := d.db.QueryRow(`
		SELECT eval_id, model_id, status, agent_output, agent_exit_code, started_at, completed_at
		FROM eval_runs WHERE id = ?
	`, id).Scan(&run.EvalID, &run.ModelID, &run.Status, &agentOutput, &run.AgentExitCode, &run.StartedAt, &completedAt)

	if err != nil {
		return nil, err
	}

	if agentOutput.Valid {
		run.AgentOutput = agentOutput.String
	}
	run.CompletedAt = completedAt
	return run, nil
}

// UpdateEvalRunStatus updates the status of an eval run
func (d *Database) UpdateEvalRunStatus(id int64, status string) error {
	query := "UPDATE eval_runs SET status = ?"
	args := []interface{}{status}

	if status == "completed" || status == "failed" {
		query += ", completed_at = ?"
		args = append(args, time.Now())
	}

	query += " WHERE id = ?"
	args = append(args, id)

	_, err := d.db.Exec(query, args...)
	return err
}

// UpdateEvalRunAgentResult updates the agent output and exit code
func (d *Database) UpdateEvalRunAgentResult(id int64, output string, exitCode int) error {
	_, err := d.db.Exec(`
		UPDATE eval_runs SET agent_output = ?, agent_exit_code = ?
		WHERE id = ?
	`, output, exitCode, id)
	return err
}

// GetEvalRuns gets all eval runs, optionally filtered by eval_id
func (d *Database) GetEvalRuns(evalID string) ([]*EvalRun, error) {
	var rows *sql.Rows
	var err error

	if evalID != "" {
		rows, err = d.db.Query(`
			SELECT id, eval_id, model_id, status, agent_output, agent_exit_code, started_at, completed_at
			FROM eval_runs WHERE eval_id = ?
			ORDER BY started_at DESC
		`, evalID)
	} else {
		rows, err = d.db.Query(`
			SELECT id, eval_id, model_id, status, agent_output, agent_exit_code, started_at, completed_at
			FROM eval_runs ORDER BY started_at DESC
		`)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*EvalRun
	for rows.Next() {
		var completedAt *time.Time
		var agentOutput sql.NullString
		run := &EvalRun{}

		err := rows.Scan(&run.ID, &run.EvalID, &run.ModelID, &run.Status, &agentOutput, &run.AgentExitCode, &run.StartedAt, &completedAt)
		if err != nil {
			return nil, err
		}

		if agentOutput.Valid {
			run.AgentOutput = agentOutput.String
		}

		run.CompletedAt = completedAt
		runs = append(runs, run)
	}

	return runs, nil
}

// Eval result operations

// CreateEvalResult creates a new eval result
func (d *Database) CreateEvalResult(result *EvalResult) error {
	_, err := d.db.Exec(`
		INSERT INTO eval_results 
		(run_id, test_case_id, passed, actual_output, expected_output, error, 
		 input_tokens, output_tokens, estimate_cost, response_time, execution_time,
		 container_name, raw_output, errors, detailed_execution_info)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, result.RunID, result.TestCaseID, result.Passed, result.ActualOutput,
		result.ExpectedOutput, result.Error, result.InputTokens, result.OutputTokens,
		result.EstimateCost, result.ResponseTime, result.ExecutionTime,
		result.ContainerName, result.RawOutput, result.Errors, result.DetailedExecutionInfo)
	return err
}

// GetEvalResults gets all results for a run
func (d *Database) GetEvalResults(runID int64) ([]EvalResult, error) {
	rows, err := d.db.Query(`
		SELECT id, run_id, test_case_id, passed, actual_output, expected_output, error,
			   input_tokens, output_tokens, estimate_cost, response_time, execution_time,
			   container_name, raw_output, errors, detailed_execution_info
		FROM eval_results WHERE run_id = ?
		ORDER BY id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []EvalResult
	for rows.Next() {
		var result EvalResult
		var containerName, rawOutput, errors, detailedExecutionInfo sql.NullString
		err := rows.Scan(
			&result.ID, &result.RunID, &result.TestCaseID, &result.Passed,
			&result.ActualOutput, &result.ExpectedOutput, &result.Error,
			&result.InputTokens, &result.OutputTokens, &result.EstimateCost,
			&result.ResponseTime, &result.ExecutionTime,
			&containerName, &rawOutput, &errors, &detailedExecutionInfo)
		if err != nil {
			return nil, err
		}

		// Handle nullable fields
		if containerName.Valid {
			result.ContainerName = containerName.String
		}
		if rawOutput.Valid {
			result.RawOutput = rawOutput.String
		}
		if errors.Valid {
			result.Errors = errors.String
		}
		if detailedExecutionInfo.Valid {
			result.DetailedExecutionInfo = detailedExecutionInfo.String
		}

		results = append(results, result)
	}

	return results, nil
}

// GetEvalStats gets aggregated stats for eval runs
func (d *Database) GetEvalStats(evalID string) ([]EvalStats, error) {
	var rows *sql.Rows
	var err error

	if evalID != "" {
		rows, err = d.db.Query(`
			SELECT 
				r.id,
				r.eval_id,
				r.model_id,
				r.status,
				COUNT(res.id) as total_count,
				SUM(CASE WHEN res.passed = 1 THEN 1 ELSE 0 END) as passed_count,
				COALESCE(SUM(res.input_tokens + res.output_tokens), 0) as total_tokens,
				COALESCE(SUM(res.estimate_cost), 0.0) as total_cost,
				COALESCE(AVG(res.response_time), 0) as avg_response_time,
				COALESCE(AVG(res.execution_time), 0) as avg_execution_time,
				r.started_at,
				r.completed_at
			FROM eval_runs r
			LEFT JOIN eval_results res ON r.id = res.run_id
			WHERE r.eval_id = ?
			GROUP BY r.id, r.eval_id, r.model_id, r.status, r.started_at, r.completed_at
			ORDER BY r.started_at DESC
		`, evalID)
	} else {
		rows, err = d.db.Query(`
			SELECT 
				r.id,
				r.eval_id,
				r.model_id,
				r.status,
				COUNT(res.id) as total_count,
				SUM(CASE WHEN res.passed = 1 THEN 1 ELSE 0 END) as passed_count,
				COALESCE(SUM(res.input_tokens + res.output_tokens), 0) as total_tokens,
				COALESCE(SUM(res.estimate_cost), 0.0) as total_cost,
				COALESCE(AVG(res.response_time), 0) as avg_response_time,
				COALESCE(AVG(res.execution_time), 0) as avg_execution_time,
				r.started_at,
				r.completed_at
			FROM eval_runs r
			LEFT JOIN eval_results res ON r.id = res.run_id
			GROUP BY r.id, r.eval_id, r.model_id, r.status, r.started_at, r.completed_at
			ORDER BY r.started_at DESC
		`)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []EvalStats
	for rows.Next() {
		var stat EvalStats
		var responseTime, executionTime float64

		err := rows.Scan(&stat.RunID, &stat.EvalID, &stat.ModelID, &stat.Status,
			&stat.TotalCount, &stat.PassedCount, &stat.TotalTokens, &stat.TotalCost,
			&responseTime, &executionTime, &stat.StartedAt, &stat.CompletedAt)
		if err != nil {
			return nil, err
		}

		stat.FailedCount = stat.TotalCount - stat.PassedCount
		stat.ResponseTime = int(responseTime)
		stat.ExecutionTime = int(executionTime)
		stat.AvgResponseTime = int(responseTime)
		stat.AvgExecutionTime = int(executionTime)

		if stat.TotalCount > 0 {
			stat.PassRate = float64(stat.PassedCount) / float64(stat.TotalCount) * 100
		}

		stats = append(stats, stat)
	}

	return stats, nil
}
