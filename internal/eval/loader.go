package eval

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Loader loads evaluation definitions from JSON files
type Loader struct {
	evalDir string
}

// NewLoader creates a new loader
func NewLoader(evalDir string) *Loader {
	return &Loader{evalDir: evalDir}
}

// LoadAll loads all eval definitions from the eval directory
func (l *Loader) LoadAll() (map[string]*EvalDefinition, error) {
	evals := make(map[string]*EvalDefinition)

	err := filepath.WalkDir(l.evalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Look for eval.json files
		if strings.ToLower(d.Name()) == "eval.json" {
			evalDef, err := l.loadEvalFile(path)
			if err != nil {
				fmt.Printf("Warning: failed to load eval from %s: %v\n", path, err)
				return nil // Continue loading other files
			}

			if evalDef != nil {
				if _, exists := evals[evalDef.ID]; exists {
					fmt.Printf("Warning: duplicate eval ID %s found in %s\n", evalDef.ID, path)
					return nil
				}
				evals[evalDef.ID] = evalDef
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk eval directory: %w", err)
	}

	return evals, nil
}

// LoadEval loads a specific eval definition by ID
func (l *Loader) LoadEval(evalID string) (*EvalDefinition, error) {
	// First try to find it in subdirectory named after the eval
	possiblePaths := []string{
		filepath.Join(l.evalDir, evalID, "eval.json"),
		filepath.Join(l.evalDir, evalID+".json"),
	}

	for _, path := range possiblePaths {
		evalDef, err := l.loadEvalFile(path)
		if err == nil && evalDef != nil {
			return evalDef, nil
		}
	}

	// If not found, search all eval.json files
	evals, err := l.LoadAll()
	if err != nil {
		return nil, err
	}

	evalDef, exists := evals[evalID]
	if !exists {
		return nil, fmt.Errorf("eval definition not found: %s", evalID)
	}

	return evalDef, nil
}

// loadEvalFile loads an eval definition from a specific file
func (l *Loader) loadEvalFile(path string) (*EvalDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	evalDef, err := LoadEvalDefinition(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse eval definition from %s: %w", path, err)
	}

	// Set the source file path for reference
	evalDef.ID = filepath.Base(filepath.Dir(path))
	if evalDef.ID == "." {
		evalDef.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	}

	return evalDef, nil
}

// ListAvailableEvals returns a list of available eval IDs
func (l *Loader) ListAvailableEvals() ([]string, error) {
	evals, err := l.LoadAll()
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(evals))
	for id := range evals {
		ids = append(ids, id)
	}

	return ids, nil
}

// ValidateDirectory checks if the eval directory exists and is readable
func (l *Loader) ValidateDirectory() error {
	info, err := os.Stat(l.evalDir)
	if err != nil {
		return fmt.Errorf("eval directory does not exist: %s", l.evalDir)
	}

	if !info.IsDir() {
		return fmt.Errorf("eval path is not a directory: %s", l.evalDir)
	}

	return nil
}
