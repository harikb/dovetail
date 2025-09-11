# Requirements Document: Advanced Directory Comparison & Sync Tool

## 1.0 Overview

This document outlines the requirements for a command-line tool that performs an advanced recursive comparison of two directories. The tool will identify differences, allow for granular filtering, and generate a human-editable "action file." This action file can then be used by the tool to synchronize the directories in a controlled, verifiable manner, with support for both a "dry run" and a final execution mode.

## 2.0 Core Features

### 2.1 Directory Comparison Engine

#### 2.1.1 Recursive Analysis
The tool must be able to recursively traverse two specified directories (`DIR_LEFT` and `DIR_RIGHT`).

#### 2.1.2 Status Identification
For every file and directory, the tool must identify its status in one of the following categories:

- **Identical**: The file exists in both locations and its content is identical.
- **Modified**: The file exists in both locations, but its content differs.
- **Only in Left**: The file or directory exists only in `DIR_LEFT`.
- **Only in Right**: The file or directory exists only in `DIR_RIGHT`.

#### 2.1.3 Content-Aware Comparison
By default, file comparison for text files will be based on content. For binary files, comparison will be based on a checksum (e.g., SHA-256 hash) to ensure accuracy.

### 2.2 Exclusion and Filtering Capabilities

The tool must provide command-line options to exclude items from the comparison. Excluded items will not appear in any output.

#### 2.2.1 Exclude by Name/Wildcard
Exclude any file or directory matching a specific name or a glob pattern (e.g., `*.log`, `__pycache__`, `node_modules`).

#### 2.2.2 Exclude by Relative Path
Exclude a specific file or directory by its full path relative to the comparison root (e.g., `src/tests/data/temp.txt`).

#### 2.2.3 Exclude by Extension
Exclude files based on their extension (e.g., `.tmp`, `.bak`). This is a specific use case of the wildcard exclusion (`*.<ext>`).

### 2.3 Difference Reporting

#### 2.3.1 Summary Report
The primary output of a comparison run is the "action file" (see section 3.0).

#### 2.3.2 Inline Diff Display
The tool must have an option to show a line-by-line, unified diff for all files identified as "Modified".

#### 2.3.3 Whitespace Handling
The inline diff display must have a sub-option to ignore differences in whitespace (leading/trailing spaces, space vs. tab, line endings).

## 3.0 Action File Workflow

The core of the tool is a three-stage workflow: **Generate → Review → Apply**.

### 3.1 Stage 1: Action File Generation

#### 3.1.1 File Format
The comparison process will generate a plain text file (`diff_list.txt`) where each line represents a difference and specifies a default action. The format will be:

```
[ACTION] : STATUS : RELATIVE_PATH
```

#### 3.1.2 Example Action File

```
# Action File generated on YYYY-MM-DD HH:MM:SS
# Left: /path/to/source_A
# Right: /path/to/source_B
#
# Actions:
#   >  : Copy file from Left to Right (overwrite)
#   <  : Copy file from Right to Left (overwrite)
#   i  : Ignore this difference, do nothing
#   x- : Delete file from Left
#   -x : Delete file from Right
#   xx : Delete file from both Left and Right
#
[i] : MODIFIED     : src/core/main.py
[i] : ONLY_IN_LEFT : assets/images/icon.png
[i] : ONLY_IN_RIGHT: docs/README.md
```

#### 3.1.3 Default State
All generated lines will have `[i]` (ignore) as the default action to ensure no accidental operations occur.

### 3.2 Stage 2: User Review and Edit
The user will manually edit the action file, changing `[i]` to the desired action (`>`, `<`, `x-`, etc.) for each item. The tool is not involved in this stage.

### 3.3 Stage 3: Execution from Action File
The tool will be able to parse an edited action file to perform operations.

#### 3.3.1 Dry-Run Mode
The tool must have a dry-run mode. When provided with an action file, it will print a verbose description of every action it would take, without modifying any files.

**Example Output:**
```
DRY RUN: Would COPY /path/to/source_A/src/core/main.py -> /path/to/source_B/src/core/main.py
```

#### 3.3.2 Execute Mode
The tool must have an apply mode that executes the actions specified in the action file. It should print a log of each action as it is performed.

## 4.0 Command-Line Interface (CLI)

The tool will be operated via a clear command-line interface.

### Generate Diff & Action File:
```bash
sync-tool diff <DIR_LEFT> <DIR_RIGHT> -o <ACTION_FILE_PATH> [--exclude-name "*.tmp" "build"] [--exclude-path "src/config.js"]
```

### Show Diffs (Optional):
```bash
sync-tool diff <DIR_LEFT> <DIR_RIGHT> --show-diff [--ignore-whitespace]
```

### Perform Dry Run:
```bash
sync-tool dry-run <ACTION_FILE_PATH> --left <DIR_LEFT> --right <DIR_RIGHT>
```

### Execute Sync:
```bash
sync-tool apply <ACTION_FILE_PATH> --left <DIR_LEFT> --right <DIR_RIGHT>
```

**Note:** Directory paths are required for dry-run and apply to resolve the relative paths from the action file into absolute, actionable paths.

## 5.0 Non-Functional Requirements

### 5.1 Performance
The tool should be reasonably performant on large directory structures. File hashing should be done efficiently.

### 5.2 Safety
The tool must never perform destructive actions (delete, overwrite) without explicit instructions from a user-edited action file. The dry-run mode is critical for verification.

### 5.3 Usability
The CLI commands, options, and output should be clear and intuitive. Help text should be available via a `--help` flag.

### 5.4 Error Handling
The tool will gracefully handle errors such as missing files (that were present during the diff but gone during apply), permission errors, and invalid action codes in the action file, reporting them clearly to the user.

