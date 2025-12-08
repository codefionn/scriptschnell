package project

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectType describes a detected project heuristic along with supporting evidence.
type ProjectType struct {
	ID          string
	Name        string
	Description string
	Evidence    []string
	Confidence  float64
}

// Detector scans a directory tree for files that indicate supported project types.
type Detector struct {
	root     string
	maxDepth int
}

// Option customizes the detector before running detection.
type Option func(*Detector)

// NewDetector creates a new detector rooted at the provided directory.
// If root is empty, the current directory is used.
func NewDetector(root string, opts ...Option) *Detector {
	d := &Detector{
		root:     root,
		maxDepth: 4,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(d)
		}
	}
	return d
}

// WithMaxDepth controls how deep the detector will traverse subdirectories.
// A non-positive value disables the depth limit.
func WithMaxDepth(depth int) Option {
	return func(d *Detector) {
		d.maxDepth = depth
	}
}

// Detect inspects the directory tree and returns matched project types ordered by confidence.
func (d *Detector) Detect(ctx context.Context) ([]ProjectType, error) {
	root := d.root
	if root == "" {
		root = "."
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	files, err := collectFiles(ctx, absRoot, d.maxDepth)
	if err != nil {
		return nil, err
	}

	result := make([]ProjectType, 0, len(definitions))
	for _, def := range definitions {
		matched, evidence, optionalMatches := def.match(files)
		if !matched {
			continue
		}

		confidence := def.BaseConfidence
		if confidence <= 0 {
			confidence = 0.7
		}
		confidence += float64(optionalMatches) * def.OptionalBonus
		if confidence > 1 {
			confidence = 1
		}

		if len(evidence) == 0 {
			evidence = append(evidence, fmt.Sprintf("matched %s heuristics", def.Name))
		}

		result = append(result, ProjectType{
			ID:          def.ID,
			Name:        def.Name,
			Description: def.Description,
			Evidence:    evidence,
			Confidence:  confidence,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Confidence == result[j].Confidence {
			return result[i].Name < result[j].Name
		}
		return result[i].Confidence > result[j].Confidence
	})

	return result, nil
}

type indicator struct {
	Pattern string
	Match   func(string) bool
}

type requirement struct {
	Indicators []indicator
}

type projectDefinition struct {
	ID                 string
	Name               string
	Description        string
	Requirements       []requirement
	OptionalIndicators []indicator
	BaseConfidence     float64
	OptionalBonus      float64
}

func (def projectDefinition) match(paths []string) (bool, []string, int) {
	evidence := make([]string, 0, len(def.Requirements)+len(def.OptionalIndicators))

	for _, req := range def.Requirements {
		satisfied := false
		for _, ind := range req.Indicators {
			matches := ind.match(paths)
			if len(matches) == 0 {
				continue
			}
			satisfied = true
			evidence = append(evidence, buildEvidence(ind.Pattern, matches)...)
			break
		}
		if !satisfied {
			return false, nil, 0
		}
	}

	optionalMatches := 0
	for _, ind := range def.OptionalIndicators {
		matches := ind.match(paths)
		if len(matches) == 0 {
			continue
		}
		optionalMatches++
		evidence = append(evidence, buildEvidence(ind.Pattern, matches)...)
	}

	return true, evidence, optionalMatches
}

func collectFiles(ctx context.Context, root string, maxDepth int) ([]string, error) {
	paths := make([]string, 0, 64)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if rel == "." {
			return nil
		}

		rel = filepath.ToSlash(rel)
		if maxDepth > 0 && depthOf(rel) > maxDepth {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if entry.IsDir() {
			return nil
		}

		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return paths, nil
}

func depthOf(rel string) int {
	clean := strings.Trim(rel, "/")
	if clean == "" {
		return 0
	}
	return strings.Count(clean, "/") + 1
}

func (ind indicator) match(paths []string) []string {
	matches := make([]string, 0, 2)
	for _, p := range paths {
		if ind.Match == nil {
			continue
		}
		if ind.Match(p) {
			matches = append(matches, p)
		}
	}
	return matches
}

func buildEvidence(pattern string, matches []string) []string {
	evidence := make([]string, 0, len(matches))
	limit := 1
	if len(matches) < limit {
		limit = len(matches)
	}
	for i := 0; i < limit; i++ {
		evidence = append(evidence, fmt.Sprintf("%s (matched %s)", matches[i], pattern))
	}
	return evidence
}

func exact(path string) indicator {
	normalized := filepath.ToSlash(path)
	return indicator{
		Pattern: normalized,
		Match: func(rel string) bool {
			return strings.EqualFold(rel, normalized)
		},
	}
}

func suffix(pattern string) indicator {
	normalized := strings.ToLower(filepath.ToSlash(pattern))
	return indicator{
		Pattern: pattern,
		Match: func(rel string) bool {
			return strings.HasSuffix(strings.ToLower(rel), normalized)
		},
	}
}

var definitions = []projectDefinition{
	{
		ID:          "go",
		Name:        "Go",
		Description: "Go module project declared via go.mod",
		Requirements: []requirement{
			{Indicators: []indicator{exact("go.mod")}},
		},
		OptionalIndicators: []indicator{
			exact("go.sum"),
		},
		BaseConfidence: 0.94,
		OptionalBonus:  0.02,
	},
	{
		ID:          "nodejs",
		Name:        "Node.js / JavaScript",
		Description: "package.json defines a JavaScript or TypeScript workspace",
		Requirements: []requirement{
			{Indicators: []indicator{exact("package.json")}},
		},
		OptionalIndicators: []indicator{
			exact("package-lock.json"),
			exact("yarn.lock"),
			exact("pnpm-lock.yaml"),
			exact("tsconfig.json"),
		},
		BaseConfidence: 0.85,
		OptionalBonus:  0.02,
	},
	{
		ID:          "rust",
		Name:        "Rust",
		Description: "Cargo project described by Cargo.toml",
		Requirements: []requirement{
			{Indicators: []indicator{exact("Cargo.toml")}},
		},
		OptionalIndicators: []indicator{
			exact("Cargo.lock"),
		},
		BaseConfidence: 0.92,
		OptionalBonus:  0.03,
	},
	{
		ID:          "python",
		Name:        "Python",
		Description: "Python project with pyproject.toml, requirements, or setup.py",
		Requirements: []requirement{
			{Indicators: []indicator{
				exact("pyproject.toml"),
				exact("requirements.txt"),
				exact("setup.py"),
			}},
		},
		OptionalIndicators: []indicator{
			exact("Pipfile"),
			exact("Pipfile.lock"),
			exact("poetry.lock"),
		},
		BaseConfidence: 0.8,
		OptionalBonus:  0.02,
	},
	{
		ID:          "csharp",
		Name:        "C# (.NET)",
		Description: "C# solution or project with .csproj or .sln",
		Requirements: []requirement{
			{Indicators: []indicator{
				suffix(".csproj"),
				suffix(".sln"),
			}},
		},
		OptionalIndicators: []indicator{
			exact("global.json"),
		},
		BaseConfidence: 0.86,
		OptionalBonus:  0.02,
	},
	{
		ID:          "java",
		Name:        "Java",
		Description: "Java project using Maven or Gradle build files",
		Requirements: []requirement{
			{Indicators: []indicator{
				exact("pom.xml"),
				suffix("build.gradle"),
				suffix("build.gradle.kts"),
			}},
		},
		OptionalIndicators: []indicator{
			exact("settings.gradle"),
		},
		BaseConfidence: 0.78,
		OptionalBonus:  0.02,
	},
	{
		ID:          "ruby",
		Name:        "Ruby",
		Description: "Ruby project governed by a Gemfile",
		Requirements: []requirement{
			{Indicators: []indicator{exact("Gemfile")}},
		},
		OptionalIndicators: []indicator{
			exact("Gemfile.lock"),
		},
		BaseConfidence: 0.75,
		OptionalBonus:  0.01,
	},
	{
		ID:          "php",
		Name:        "PHP",
		Description: "PHP project with composer.json or composer.lock",
		Requirements: []requirement{
			{Indicators: []indicator{
				exact("composer.json"),
				exact("composer.lock"),
			}},
		},
		OptionalIndicators: []indicator{
			exact("vendor/"),
		},
		BaseConfidence: 0.82,
		OptionalBonus:  0.02,
	},
	{
		ID:          "swift",
		Name:        "Swift",
		Description: "Swift project with Package.swift or Xcode project files",
		Requirements: []requirement{
			{Indicators: []indicator{
				exact("Package.swift"),
				suffix(".xcodeproj"),
				suffix(".xcworkspace"),
			}},
		},
		OptionalIndicators: []indicator{
			exact("Package.resolved"),
		},
		BaseConfidence: 0.88,
		OptionalBonus:  0.03,
	},
	{
		ID:          "kotlin",
		Name:        "Kotlin",
		Description: "Kotlin project with build.gradle.kts or .kt files",
		Requirements: []requirement{
			{Indicators: []indicator{
				suffix("build.gradle.kts"),
				suffix(".kt"),
			}},
		},
		OptionalIndicators: []indicator{
			exact("settings.gradle.kts"),
		},
		BaseConfidence: 0.79,
		OptionalBonus:  0.02,
	},
	{
		ID:          "scala",
		Name:        "Scala",
		Description: "Scala project with build.sbt or .scala files",
		Requirements: []requirement{
			{Indicators: []indicator{
				exact("build.sbt"),
				suffix(".scala"),
				suffix(".sbt"),
			}},
		},
		OptionalIndicators: []indicator{
			exact("project/build.properties"),
		},
		BaseConfidence: 0.81,
		OptionalBonus:  0.02,
	},
	{
		ID:          "dart",
		Name:        "Dart",
		Description: "Dart/Flutter project with pubspec.yaml",
		Requirements: []requirement{
			{Indicators: []indicator{exact("pubspec.yaml")}},
		},
		OptionalIndicators: []indicator{
			exact("pubspec.lock"),
			exact("lib/main.dart"),
		},
		BaseConfidence: 0.87,
		OptionalBonus:  0.03,
	},
	{
		ID:          "cpp",
		Name:        "C++",
		Description: "C++ project with CMakeLists.txt or Makefile",
		Requirements: []requirement{
			{Indicators: []indicator{
				exact("CMakeLists.txt"),
				exact("Makefile"),
				suffix(".cmake"),
			}},
		},
		OptionalIndicators: []indicator{
			exact("CMakeCache.txt"),
		},
		BaseConfidence: 0.83,
		OptionalBonus:  0.02,
	},
	{
		ID:          "c",
		Name:        "C",
		Description: "C project with Makefile or configure script",
		Requirements: []requirement{
			{Indicators: []indicator{
				exact("Makefile"),
				exact("configure"),
				exact("CMakeLists.txt"),
			}},
		},
		OptionalIndicators: []indicator{
			exact("config.h.in"),
		},
		BaseConfidence: 0.76,
		OptionalBonus:  0.01,
	},
}
