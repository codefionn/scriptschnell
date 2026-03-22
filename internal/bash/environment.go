package bash

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Environment manages shell environment variables
type Environment struct {
	variables map[string]*Variable
	parent    *Environment
	mu        sync.RWMutex
	readonly  map[string]bool
	exported  map[string]bool
}

// Variable represents a shell variable with metadata
type Variable struct {
	Name   string      `json:"name"`
	Value  string      `json:"value"`
	Export bool        `json:"export"`    // Whether to export to child processes
	Local  bool        `json:"local"`     // Whether it's a local variable
	Readonly bool      `json:"readonly"`  // Whether it's readonly
	Type   VarType     `json:"type"`      // Variable type
	Array  []string    `json:"array"`     // Array value (for array variables)
	Function *Function `json:"function"`  // Function value (for function variables)
}

// VarType represents the type of a variable
type VarType string

const (
	VarString   VarType = "string"   // Regular string variable
	VarArray    VarType = "array"    // Array variable
	VarInteger  VarType = "integer"  // Integer variable (declare -i)
	VarFunction VarType = "function" // Function variable
	VarReadonly VarType = "readonly" // Readonly variable
)

// Function represents a shell function
type Function struct {
	Name string `json:"name"`
	Body Node   `json:"body"` // Function body AST
	Source string `json:"source"` // Original source text
}

// NewEnvironment creates a new environment
func NewEnvironment() *Environment {
	env := &Environment{
		variables: make(map[string]*Variable),
		readonly:  make(map[string]bool),
		exported:  make(map[string]bool),
	}
	
	// Initialize with system environment
	for _, pair := range os.Environ() {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			env.Set(parts[0], parts[1])
			env.Export(parts[0], true)
		}
	}
	
	return env
}

// NewChildEnvironment creates a child environment (subshell)
func NewChildEnvironment(parent *Environment) *Environment {
	return &Environment{
		variables: make(map[string]*Variable),
		parent:    parent,
		readonly:  make(map[string]bool),
		exported:  make(map[string]bool),
	}
}

// Set sets a variable value
func (e *Environment) Set(name, value string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Check if readonly
	if e.isReadonlyLocked(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	
	e.variables[name] = &Variable{
		Name:  name,
		Value: value,
		Type:  VarString,
	}
	
	return nil
}

// SetArray sets an array variable
func (e *Environment) SetArray(name string, values []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.isReadonlyLocked(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	
	e.variables[name] = &Variable{
		Name:  name,
		Array: values,
		Type:  VarArray,
	}
	
	return nil
}

// SetFunction sets a function variable
func (e *Environment) SetFunction(name string, fn *Function) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.isReadonlyLocked(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	
	e.variables[name] = &Variable{
		Name:     name,
		Function: fn,
		Type:     VarFunction,
	}
	
	return nil
}

// Get gets a variable value
func (e *Environment) Get(name string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	return e.getLocked(name)
}

// GetVariable gets a variable with all metadata
func (e *Environment) GetVariable(name string) (*Variable, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	return e.getVariableLocked(name)
}

func (e *Environment) getLocked(name string) (string, bool) {
	// Check current environment first
	if v, ok := e.variables[name]; ok {
		switch v.Type {
		case VarArray:
			// For arrays, return first element or all elements joined
			if len(v.Array) > 0 {
				return v.Array[0], true
			}
			return "", true
		case VarFunction:
			return v.Function.Source, true
		default:
			return v.Value, true
		}
	}
	
	// Check parent environment
	if e.parent != nil {
		return e.parent.Get(name)
	}
	
	return "", false
}

func (e *Environment) getVariableLocked(name string) (*Variable, bool) {
	if v, ok := e.variables[name]; ok {
		return v, true
	}
	
	if e.parent != nil {
		return e.parent.GetVariable(name)
	}
	
	return nil, false
}

func (e *Environment) isReadonlyLocked(name string) bool {
	if e.readonly[name] {
		return true
	}
	
	if v, ok := e.getVariableLocked(name); ok {
		return v.Readonly
	}
	
	return false
}

// Delete deletes a variable
func (e *Environment) Delete(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.isReadonlyLocked(name) {
		return fmt.Errorf("%s: readonly variable", name)
	}
	
	delete(e.variables, name)
	delete(e.exported, name)
	
	return nil
}

// Export marks a variable for export to child processes
func (e *Environment) Export(name string, export bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// If setting export, ensure variable exists
	if export {
		if _, ok := e.getVariableLocked(name); !ok {
			// Create variable with empty value
			e.variables[name] = &Variable{
				Name:  name,
				Value: "",
				Type:  VarString,
			}
		}
	}
	
	e.exported[name] = export
	
	return nil
}

// IsExported checks if a variable is exported
func (e *Environment) IsExported(name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	if e.exported[name] {
		return true
	}
	
	if v, ok := e.getVariableLocked(name); ok {
		return v.Export
	}
	
	return false
}

// SetReadonly marks a variable as readonly
func (e *Environment) SetReadonly(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Ensure variable exists
	if _, ok := e.getVariableLocked(name); !ok {
		// Create variable with empty value
		e.variables[name] = &Variable{
			Name:  name,
			Value: "",
			Type:  VarString,
		}
	}
	
	e.readonly[name] = true
	
	if v, ok := e.variables[name]; ok {
		v.Readonly = true
		v.Type = VarReadonly
	}
	
	return nil
}

// Unset removes a variable (respects readonly)
func (e *Environment) Unset(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.isReadonlyLocked(name) {
		return fmt.Errorf("%s: cannot unset: readonly variable", name)
	}
	
	delete(e.variables, name)
	delete(e.exported, name)
	delete(e.readonly, name)
	
	return nil
}

// List lists all variables
func (e *Environment) List() []*Variable {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	vars := make([]*Variable, 0, len(e.variables))
	for _, v := range e.variables {
		vars = append(vars, v)
	}
	
	// Add parent variables
	if e.parent != nil {
		parentVars := e.parent.List()
		for _, pv := range parentVars {
			if _, ok := e.variables[pv.Name]; !ok {
				vars = append(vars, pv)
			}
		}
	}
	
	return vars
}

// ListExported lists all exported variables
func (e *Environment) ListExported() map[string]string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	result := make(map[string]string)
	
	// Add parent exported variables first
	if e.parent != nil {
		parentExported := e.parent.ListExported()
		for k, v := range parentExported {
			result[k] = v
		}
	}
	
	// Add/override with current environment
	for name := range e.exported {
		if v, ok := e.getLocked(name); ok {
			result[name] = v
		}
	}
	
	// Also check variables marked as exported
	for _, v := range e.variables {
		if v.Export {
			result[v.Name] = v.Value
		}
	}
	
	return result
}

// ToSlice converts exported variables to slice format for exec
func (e *Environment) ToSlice() []string {
	exported := e.ListExported()
	result := make([]string, 0, len(exported))
	for name, value := range exported {
		result = append(result, name+"="+value)
	}
	return result
}

// Expand expands variables in a string using the environment
func (e *Environment) Expand(s string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	return os.Expand(s, func(name string) string {
		if value, ok := e.getLocked(name); ok {
			return value
		}
		return ""
	})
}

// ExpandWithDefault expands a variable with a default value
func (e *Environment) ExpandWithDefault(name, def string) string {
	if value, ok := e.Get(name); ok && value != "" {
		return value
	}
	return def
}

// ExpandWithError expands a variable or returns an error message
func (e *Environment) ExpandWithError(name, errorMsg string) (string, error) {
	if value, ok := e.Get(name); ok && value != "" {
		return value, nil
	}
	return "", fmt.Errorf("%s: %s", name, errorMsg)
}

// ExpandAlternative expands to alternative value if variable is set
func (e *Environment) ExpandAlternative(name, alt string) string {
	if value, ok := e.Get(name); ok && value != "" {
		return alt
	}
	return ""
}

// SpecialVariables provides access to bash special variables
type SpecialVariables struct {
	env *Environment
}

// GetSpecial returns the value of a special variable
// $? - Exit status of last command
// $$ - Process ID of shell
// $! - Process ID of last background command
// $0 - Name of shell or script
// $# - Number of positional parameters
// $@ - All positional parameters as separate strings
// $* - All positional parameters as single string
// $- - Current options set for the shell
// $_ - Last argument to previous command
func (s *SpecialVariables) Get(name string) (string, bool) {
	switch name {
	case "?":
		// Exit status - would need to be set by executor
		if v, ok := s.env.Get("?"); ok {
			return v, ok
		}
		return "0", true
	case "$":
		return fmt.Sprintf("%d", os.Getpid()), true
	case "!":
		// Last background PID - would need to be set by executor
		if v, ok := s.env.Get("!"); ok {
			return v, ok
		}
		return "", false
	case "0":
		if v, ok := s.env.Get("0"); ok {
			return v, ok
		}
		return "bash", true
	default:
		return "", false
	}
}

// PositionalParameters manages positional parameters ($1, $2, etc.)
type PositionalParameters struct {
	params []string
}

// NewPositionalParameters creates positional parameters from arguments
func NewPositionalParameters(args []string) *PositionalParameters {
	return &PositionalParameters{
		params: args,
	}
}

// Get returns the parameter at position n (1-indexed)
func (p *PositionalParameters) Get(n int) (string, bool) {
	if n < 1 || n > len(p.params) {
		return "", false
	}
	return p.params[n-1], true
}

// All returns all parameters
func (p *PositionalParameters) All() []string {
	return p.params
}

// Count returns the number of parameters
func (p *PositionalParameters) Count() int {
	return len(p.params)
}

// Set sets positional parameters (shift)
func (p *PositionalParameters) Set(args []string) {
	p.params = args
}

// Shift removes the first n parameters
func (p *PositionalParameters) Shift(n int) {
	if n >= len(p.params) {
		p.params = nil
	} else {
		p.params = p.params[n:]
	}
}
