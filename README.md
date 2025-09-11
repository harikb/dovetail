# Dovetail - Advanced Directory Comparison & Synchronization Tool

Dovetail is a powerful command-line tool that performs advanced recursive comparison of two directories, identifies differences with granular filtering capabilities, and generates human-editable "action files" for controlled synchronization.

## Features

- **Recursive Directory Comparison**: Deep comparison of entire directory trees
- **Content-Aware Analysis**: SHA-256 checksums for binary files, line-by-line for text files
- **Granular Filtering**: Exclude files by name, path, extension, or glob patterns
- **Human-Editable Workflow**: Three-stage process (Generate → Review → Apply)
- **Safety First**: All actions default to "ignore" to prevent accidents
- **Comprehensive Diff Display**: Unified diff view with whitespace handling
- **Dry-Run Mode**: Preview all actions before execution
- **Detailed Logging**: Complete audit trail of all operations

## Installation

### Build from Source

```bash
git clone https://github.com/harikb/dovetail.git
cd dovetail
go build -o dovetail
```

### Direct Go Install

```bash
go install github.com/harikb/dovetail@latest
```

## Quick Start

### 1. Generate Action File

Compare two directories and create an action file:

```bash
dovetail diff /path/to/source /path/to/target -o actions.txt
```

### 2. Edit Action File

Open `actions.txt` in your favorite editor and change `[i]` to desired actions:
- `[>]` - Copy from left to right
- `[<]` - Copy from right to left  
- `[x-]` - Delete from left
- `[-x]` - Delete from right
- `[xx]` - Delete from both
- `[i]` - Ignore (default)

### 3. Preview Changes

```bash
dovetail dry-run actions.txt -l /path/to/source -r /path/to/target
```

### 4. Execute Actions

```bash
dovetail apply actions.txt -l /path/to/source -r /path/to/target
```

## Usage Examples

### Basic Directory Comparison

```bash
# Generate action file
dovetail diff ./project-v1 ./project-v2 -o sync-actions.txt

# Preview what would happen
dovetail dry-run sync-actions.txt -l ./project-v1 -r ./project-v2

# Execute the synchronization
dovetail apply sync-actions.txt -l ./project-v1 -r ./project-v2
```

### With Filtering Options

```bash
# Exclude specific files and directories
dovetail diff /src /backup -o actions.txt \\
  --exclude-name "*.log" "*.tmp" "node_modules" \\
  --exclude-path "build/" "dist/" \\
  --exclude-ext "bak" "swp"
```

### Show Inline Diffs

```bash
# Display unified diffs instead of generating action file
dovetail diff /src /backup --show-diff --ignore-whitespace
```

### With Verbose Output

```bash
# Basic verbose - shows high-level progress
dovetail diff /src /backup -o actions.txt -v

# Detailed verbose - shows directory scanning and file counts
dovetail diff /large/project /backup -o actions.txt -vv

# Debug verbose - shows every file being processed (useful for debugging stuck operations)
dovetail diff /huge/codebase /backup -o actions.txt -vvv
```

**Tip**: Use verbose modes to monitor progress on large directory comparisons. If the tool appears stuck, higher verbosity levels will show exactly which files or directories are being processed, helping you identify problematic areas to exclude.

## Command Reference

### Global Flags

- `--verbose, -v`: Progressive verbosity levels:
  - `-v`: Basic verbose (high-level progress and summaries)
  - `-vv`: Detailed verbose (directory scanning, periodic progress)
  - `-vvv`: Debug verbose (every file processed, real-time updates)
- `--no-color`: Disable colored output
- `--config`: Specify config file (default: `$HOME/.dovetail.yaml`)

### diff Command

Compare two directories and generate an action file.

```bash
dovetail diff <LEFT_DIR> <RIGHT_DIR> [flags]
```

**Flags:**
- `-o, --output`: Output action file path (required unless --show-diff)
- `--show-diff`: Display inline diffs instead of generating action file
- `--ignore-whitespace`: Ignore whitespace differences in diffs
- `--exclude-name`: Exclude files/directories by name or glob pattern
- `--exclude-path`: Exclude files/directories by relative path
- `--exclude-ext`: Exclude files by extension (without dot)

**Examples:**
```bash
dovetail diff /src /dst -o actions.txt
dovetail diff ./code ./backup --show-diff --ignore-whitespace
dovetail diff /proj /backup --exclude-name "*.log" "build" --exclude-ext "tmp"
```

### dry-run Command

Preview actions from an action file without executing them.

```bash
dovetail dry-run <ACTION_FILE> --left <LEFT_DIR> --right <RIGHT_DIR>
```

**Flags:**
- `-l, --left`: Left directory path (required)
- `-r, --right`: Right directory path (required)

### apply Command

Execute actions from an action file.

```bash
dovetail apply <ACTION_FILE> --left <LEFT_DIR> --right <RIGHT_DIR> [flags]
```

**Flags:**
- `-l, --left`: Left directory path (required)
- `-r, --right`: Right directory path (required)
- `--force`: Skip confirmation prompt

## Action File Format

Action files are plain text files with a simple format:

```
# Action File generated on 2024-01-15 14:30:00
# Left:  /path/to/source
# Right: /path/to/target
#
# Actions:
#   i   : Ignore this difference, do nothing
#   >   : Copy file from Left to Right (overwrite)
#   <   : Copy file from Right to Left (overwrite)
#   x-  : Delete file from Left
#   -x  : Delete file from Right
#   xx  : Delete file from both Left and Right

[i] : MODIFIED      : src/main.py  # L:1.2KB R:1.3KB
[i] : ONLY_IN_LEFT  : docs/old.md  # Size: 2.1KB
[i] : ONLY_IN_RIGHT : README.md    # Size: 1.8KB
```

### File Statuses

- `IDENTICAL`: File exists in both locations with identical content
- `MODIFIED`: File exists in both locations but content differs
- `ONLY_IN_LEFT`: File exists only in the left directory
- `ONLY_IN_RIGHT`: File exists only in the right directory

### Action Types

- `[i]` **Ignore**: Do nothing (default for safety)
- `[>]` **Copy to Right**: Copy file from left to right directory
- `[<]` **Copy to Left**: Copy file from right to left directory
- `[x-]` **Delete Left**: Delete file from left directory only
- `[-x]` **Delete Right**: Delete file from right directory only
- `[xx]` **Delete Both**: Delete file from both directories

## Safety Features

1. **Default Ignore**: All actions default to `[i]` (ignore) to prevent accidental operations
2. **Validation**: Action files are validated before execution
3. **Confirmation**: Interactive confirmation before applying changes (unless `--force`)
4. **Dry-Run Mode**: Always test with `dry-run` before `apply`
5. **Error Handling**: Graceful handling of missing files and permission errors

## Configuration

Dovetail looks for configuration in `$HOME/.dovetail.yaml`:

```yaml
verbose: false
no-color: false
max-file-size: 1048576  # 1MB limit for diff display
parallel-workers: 4     # Number of parallel workers for hashing
```

## Performance Tips

- Use filtering options to exclude unnecessary files
- For very large directories, consider breaking the comparison into smaller chunks
- The tool uses parallel processing for file hashing - adjust `parallel-workers` if needed
- Large files (>1MB by default) use size+timestamp for comparison instead of content hashing

## Error Handling

Dovetail handles various error conditions gracefully:

- **Missing Files**: Files that existed during comparison but are missing during apply
- **Permission Errors**: Insufficient permissions for file operations
- **Invalid Actions**: Malformed action files with syntax errors
- **Disk Space**: Insufficient space for copy operations
- **Network Issues**: Problems with network-mounted directories

All errors are logged with clear messages and suggested solutions.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

- **Issues**: [GitHub Issues](https://github.com/harikb/dovetail/issues)
- **Discussions**: [GitHub Discussions](https://github.com/harikb/dovetail/discussions)

---

**⚠️ Important**: Always run `dovetail dry-run` before `dovetail apply` to preview changes. Dovetail can modify and delete files - use with caution!