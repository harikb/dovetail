# Changelog

## v1.0.1 - Enhanced Verbosity and Progress Monitoring

### Added
- **Progressive Verbosity Levels**: Added support for multiple verbosity levels:
  - `-v` (Level 1): Basic progress monitoring - shows high-level operations and completion summaries
  - `-vv` (Level 2): Detailed progress - shows directory scanning, file counting, and periodic progress updates
  - `-vvv` (Level 3): Debug level - shows every file being processed, hash calculations, and real-time comparison progress

### Changed
- **Verbose Flag**: Changed from simple boolean flag to count flag to support progressive levels
- **Progress Reporting**: Added real-time progress reporting during directory scanning and file comparison
- **Directory Scanning**: Enhanced with progress updates showing which directories are being processed
- **File Processing**: Added progress indicators showing current file being processed (helpful for identifying stuck operations)

### Technical Details
- New `util.ProgressReporter` class for managing progress updates at different verbosity levels
- Enhanced comparison engine with verbosity-aware logging
- Progress messages are output to stderr to avoid interfering with file output redirection
- Configurable progress reporting intervals based on verbosity level

### Examples

**Basic Verbose (-v)**:
```bash
dovetail diff /large/dir1 /large/dir2 -o actions.txt -v
```
Shows: High-level progress, scan summaries, completion status

**Detailed Verbose (-vv)**:
```bash  
dovetail diff /large/dir1 /large/dir2 -o actions.txt -vv
```
Shows: Directory scanning details, periodic file counts, progress indicators

**Debug Verbose (-vvv)**:
```bash
dovetail diff /large/dir1 /large/dir2 -o actions.txt -vvv
```
Shows: Every file being processed, hash calculations, real-time comparison status

This enhancement addresses the issue where users couldn't see what the tool was processing, making it easier to identify problematic directories and monitor progress on large directory trees.
