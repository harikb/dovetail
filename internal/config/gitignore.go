package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitignoreParser handles parsing .gitignore files with feature validation
type GitignoreParser struct {
	verboseLevel int
}

// NewGitignoreParser creates a new gitignore parser
func NewGitignoreParser(verboseLevel int) *GitignoreParser {
	return &GitignoreParser{
		verboseLevel: verboseLevel,
	}
}

// GitignoreResult contains the parsed exclusions from .gitignore files
type GitignoreResult struct {
	Names      []string // Patterns for --exclude-name
	Paths      []string // Patterns for --exclude-path
	Extensions []string // Patterns for --exclude-ext
	Sources    []string // Source files for debugging
}

// ParseGitignoreFiles reads and parses .gitignore files from the specified directories
func (p *GitignoreParser) ParseGitignoreFiles(leftDir, rightDir string, checkBothSides bool) (*GitignoreResult, error) {
	result := &GitignoreResult{
		Names:      []string{},
		Paths:      []string{},
		Extensions: []string{},
		Sources:    []string{},
	}

	// Parse left directory .gitignore
	leftGitignore := filepath.Join(leftDir, ".gitignore")
	if _, err := os.Stat(leftGitignore); err == nil {
		if err := p.parseGitignoreFile(leftGitignore, result); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", leftGitignore, err)
		}
		result.Sources = append(result.Sources, leftGitignore)
		if p.verboseLevel >= 2 {
			fmt.Fprintf(os.Stderr, "Parsed .gitignore: %s\n", leftGitignore)
		}
	}

	// Parse right directory .gitignore if requested and different from left
	if checkBothSides {
		rightGitignore := filepath.Join(rightDir, ".gitignore")
		if rightGitignore != leftGitignore {
			if _, err := os.Stat(rightGitignore); err == nil {
				if err := p.parseGitignoreFile(rightGitignore, result); err != nil {
					return nil, fmt.Errorf("failed to parse %s: %w", rightGitignore, err)
				}
				result.Sources = append(result.Sources, rightGitignore)
				if p.verboseLevel >= 2 {
					fmt.Fprintf(os.Stderr, "Parsed .gitignore: %s\n", rightGitignore)
				}
			}
		}
	}

	if p.verboseLevel >= 1 && len(result.Sources) > 0 {
		fmt.Fprintf(os.Stderr, "Applied .gitignore patterns from: %s\n", strings.Join(result.Sources, ", "))
		if p.verboseLevel >= 2 {
			p.logParsedPatterns(result)
		}
	}

	return result, nil
}

// parseGitignoreFile parses a single .gitignore file
func (p *GitignoreParser) parseGitignoreFile(path string, result *GitignoreResult) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for unsupported patterns and fail loudly
		if err := p.validatePattern(line, path, lineNumber); err != nil {
			return err
		}

		// Parse supported patterns
		p.parsePattern(line, result)
	}

	return scanner.Err()
}

// validatePattern checks if a pattern is supported and fails loudly if not
func (p *GitignoreParser) validatePattern(pattern, filePath string, lineNumber int) error {
	// Unsupported: Negation patterns
	if strings.HasPrefix(pattern, "!") {
		return &UnsupportedPatternError{
			Pattern:    pattern,
			FilePath:   filePath,
			LineNumber: lineNumber,
			Reason:     "Negation patterns (!) are not supported",
			Suggestion: "Remove the negation pattern or disable --use-gitignore",
		}
	}

	// Unsupported: Complex glob patterns
	if strings.Contains(pattern, "**") {
		return &UnsupportedPatternError{
			Pattern:    pattern,
			FilePath:   filePath,
			LineNumber: lineNumber,
			Reason:     "Double-asterisk (**) glob patterns are not supported",
			Suggestion: "Use simpler patterns like 'dirname/' or '*.ext'",
		}
	}

	// Unsupported: Character classes
	if strings.Contains(pattern, "[") && strings.Contains(pattern, "]") {
		return &UnsupportedPatternError{
			Pattern:    pattern,
			FilePath:   filePath,
			LineNumber: lineNumber,
			Reason:     "Character class patterns ([abc]) are not supported",
			Suggestion: "Use specific patterns or wildcard patterns",
		}
	}

	// Unsupported: Brace expansion
	if strings.Contains(pattern, "{") && strings.Contains(pattern, "}") {
		return &UnsupportedPatternError{
			Pattern:    pattern,
			FilePath:   filePath,
			LineNumber: lineNumber,
			Reason:     "Brace expansion patterns ({a,b}) are not supported",
			Suggestion: "Use separate patterns for each alternative",
		}
	}

	return nil
}

// parsePattern converts a gitignore pattern to dovetail exclusion patterns
func (p *GitignoreParser) parsePattern(pattern string, result *GitignoreResult) {
	original := pattern

	// Remove leading slash for root-relative patterns
	if strings.HasPrefix(pattern, "/") {
		pattern = pattern[1:]
	}

	// Directory patterns (end with /)
	if strings.HasSuffix(pattern, "/") {
		// This is a directory exclusion
		dirName := strings.TrimSuffix(pattern, "/")
		if strings.Contains(dirName, "/") {
			// Path-based exclusion: "path/to/dir/" -> --exclude-path "path/to/dir/"
			result.Paths = append(result.Paths, pattern)
		} else {
			// Name-based exclusion: "dirname/" -> --exclude-name "dirname"
			result.Names = append(result.Names, dirName)
		}
		return
	}

	// File extension patterns
	if strings.HasPrefix(pattern, "*.") && !strings.Contains(pattern[2:], "/") && !strings.Contains(pattern[2:], "*") {
		// Simple extension pattern: "*.log" -> --exclude-name "*.log"
		result.Names = append(result.Names, pattern)
		return
	}

	// Path-based patterns (contains /)
	if strings.Contains(pattern, "/") {
		// Path exclusion: "build/output" -> --exclude-path "build/output"
		result.Paths = append(result.Paths, pattern)
		return
	}

	// Simple filename patterns
	result.Names = append(result.Names, pattern)

	if p.verboseLevel >= 3 {
		fmt.Fprintf(os.Stderr, "Gitignore pattern: '%s' -> dovetail exclusion\n", original)
	}
}

// logParsedPatterns logs the patterns that were parsed (for debugging)
func (p *GitignoreParser) logParsedPatterns(result *GitignoreResult) {
	if len(result.Names) > 0 {
		fmt.Fprintf(os.Stderr, "  Names: %s\n", strings.Join(result.Names, ", "))
	}
	if len(result.Paths) > 0 {
		fmt.Fprintf(os.Stderr, "  Paths: %s\n", strings.Join(result.Paths, ", "))
	}
	if len(result.Extensions) > 0 {
		fmt.Fprintf(os.Stderr, "  Extensions: %s\n", strings.Join(result.Extensions, ", "))
	}
}

// UnsupportedPatternError represents an unsupported .gitignore pattern
type UnsupportedPatternError struct {
	Pattern    string
	FilePath   string
	LineNumber int
	Reason     string
	Suggestion string
}

func (e *UnsupportedPatternError) Error() string {
	return fmt.Sprintf(`Unsupported .gitignore pattern in %s:%d
  Pattern: "%s"
  Reason: %s
  Suggestion: %s

Supported .gitignore patterns:
  ✓ filename          (file/directory name exclusion)
  ✓ *.ext             (file extension exclusion)  
  ✓ dirname/          (directory exclusion)
  ✓ path/to/file      (path-based exclusion)
  ✓ /root-relative    (root-relative path exclusion)
  
Unsupported patterns:
  ✗ !negation         (negation patterns)
  ✗ **/*.ext          (double-asterisk globs)
  ✗ [abc].txt         (character classes)
  ✗ {a,b}.txt         (brace expansion)

Either remove the unsupported pattern or disable --use-gitignore`,
		e.FilePath, e.LineNumber, e.Pattern, e.Reason, e.Suggestion)
}
