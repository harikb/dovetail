# Implementation Plan: Advanced Directory Comparison & Sync Tool in Go

## Project Structure
```
difftool/
├── cmd/
│   ├── root.go          # Root command setup
│   ├── diff.go          # Diff command implementation
│   ├── dryrun.go        # Dry-run command
│   └── apply.go         # Apply command
├── internal/
│   ├── compare/         # Directory comparison engine
│   │   ├── engine.go    # Core comparison logic
│   │   ├── file.go      # File comparison utilities
│   │   └── filter.go    # Filtering/exclusion logic
│   ├── action/          # Action file handling
│   │   ├── generator.go # Action file generation
│   │   ├── parser.go    # Action file parsing
│   │   └── executor.go  # Action execution
│   └── diff/            # Diff display utilities
│       └── display.go   # Unified diff generation
├── go.mod
├── go.sum
├── main.go              # Entry point
├── .gitignore
└── README.md
```

## Core Components to Implement

### 1. CLI Structure (using Cobra)
- Root command with global flags
- `diff` subcommand for comparison and action file generation
- `dry-run` subcommand for preview mode
- `apply` subcommand for execution mode

### 2. Directory Comparison Engine
- Recursive directory traversal
- File content comparison (SHA-256 for binary, line-by-line for text)
- Status categorization (Identical, Modified, Only in Left/Right)
- Filtering system for exclusions

### 3. Action File System
- Generate action files with default `[i]` actions
- Parse user-edited action files
- Execute actions with proper error handling

### 4. Safety Features
- All actions default to ignore
- Comprehensive dry-run mode
- Detailed logging and error reporting

## Major Go Packages

### CLI Framework
- **github.com/spf13/cobra** - The dominant CLI framework in Go (177k+ imports)
- **github.com/spf13/viper** - Configuration management, integrates perfectly with Cobra

### File Operations & Comparison
- **crypto/sha256** - Built-in package for file checksums
- **path/filepath** - Built-in for path manipulation and walking directories
- **os** - Built-in for file operations
- **github.com/sergi/go-diff** - For generating unified diffs

### Text Processing & Output
- **text/tabwriter** - Built-in for formatted output
- **fmt** and **log** - Built-in for output and logging
- **bufio** - Built-in for efficient file reading

### Pattern Matching
- **path/filepath.Match** - Built-in for glob pattern matching
- **strings** - Built-in for string operations

### Utilities
- **github.com/fatih/color** - For colored terminal output (optional)
- **time** - Built-in for timestamps

## Implementation Steps

1. **Initialize Go module and project structure**
   - Create go.mod with module declaration
   - Set up directory structure
   - Create .gitignore file

2. **Set up Cobra CLI framework with subcommands**
   - Install Cobra and Viper dependencies
   - Create root command with global flags
   - Implement diff, dry-run, and apply subcommands

3. **Implement directory comparison engine**
   - Build recursive directory traversal
   - Create file comparison logic (content vs checksum)
   - Implement status categorization system

4. **Build filtering and exclusion system**
   - Name/wildcard pattern matching
   - Relative path exclusions
   - Extension-based filtering

5. **Create action file generation and parsing**
   - Generate action files with proper format
   - Parse user-edited action files
   - Validate action syntax

6. **Implement dry-run and execution modes**
   - Dry-run preview with detailed output
   - Safe execution with progress logging
   - Rollback capabilities for failed operations

7. **Add comprehensive error handling and logging**
   - Graceful error handling for missing files
   - Permission error handling
   - Clear user feedback and progress indicators

8. **Create .gitignore and documentation**
   - Comprehensive .gitignore for Go projects
   - Usage documentation and examples
   - API documentation for internal packages

## CLI Commands Structure

### Generate Diff & Action File:
```bash
difftool diff <DIR_LEFT> <DIR_RIGHT> -o <ACTION_FILE_PATH> [--exclude-name "*.tmp" "build"] [--exclude-path "src/config.js"]
```

### Show Diffs (Optional):
```bash
difftool diff <DIR_LEFT> <DIR_RIGHT> --show-diff [--ignore-whitespace]
```

### Perform Dry Run:
```bash
difftool dry-run <ACTION_FILE_PATH> --left <DIR_LEFT> --right <DIR_RIGHT>
```

### Execute Sync:
```bash
difftool apply <ACTION_FILE_PATH> --left <DIR_LEFT> --right <DIR_RIGHT>
```

## Safety and Performance Considerations

- Default all actions to ignore (`[i]`) to prevent accidental operations
- Implement comprehensive dry-run mode for verification
- Use efficient file hashing for large files
- Handle permission errors gracefully
- Provide clear progress indicators for long operations
- Support interruption and cleanup for partial operations