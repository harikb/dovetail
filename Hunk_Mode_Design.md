# Hunk Mode Design for Diff Tool

## Overview

This document describes the proposed behavior for **Hunk Mode** - a feature that allows users to selectively apply individual hunks from a unified diff, rather than applying entire files.

## Core Concepts

### What is Hunk Mode?
- **View**: Shows a unified diff between two files with individual hunks highlighted
- **Navigation**: User can navigate between hunks using `n`/`p` (next/previous)
- **Selective Application**: User can apply specific hunks rather than the entire diff
- **Direction Control**: User controls which file gets modified

### Key Principles
1. **WYSIWYG**: What you see is exactly what gets applied - no hidden transformations
2. **Explicit Direction**: User explicitly controls diff direction and target file
3. **Single Action**: Only one apply action (`<`) to avoid confusion
4. **Visual Feedback**: Clear indication of current direction and applied hunks

## User Workflow

### Entering Hunk Mode
1. User views a file comparison in normal diff view
2. Presses `SPACE` or `ENTER` to enter Hunk Mode
3. Diff is parsed into individual hunks
4. First hunk is highlighted

### Navigation
- `n` / `↓`: Move to next hunk
- `p` / `↑`: Move to previous hunk  
- Visual highlighting shows current hunk

### Direction Control
- **Default**: Shows `diff -u LEFT_FILE RIGHT_FILE`
- **Reverse**: Press `r` to toggle to `diff -u RIGHT_FILE LEFT_FILE`
- **Screen Refresh**: When direction changes, screen refreshes with new diff
- **Header**: Shows current direction clearly (e.g., "LEFT → RIGHT" or "RIGHT → LEFT")

### Applying Hunks
- **Single Action**: Only `<` key applies hunks
- **Target File**: Always applies to the **first file** in the current diff
- **Examples**:
  - Normal view (`diff -u left right`): `<` modifies LEFT file (LEFT becomes like RIGHT)
  - Reversed view (`diff -u right left`): `<` modifies RIGHT file (RIGHT becomes like LEFT)

### Visual Feedback
- **Applied Hunks**: Mark hunks that have been applied with visual indicator
- **Direction Display**: Header shows "Hunk Mode: filename.txt (LEFT → RIGHT) (Hunk 2 of 5)"
- **Status Messages**: Show feedback like "Applied hunk 2/5 (left→right)"

## Technical Behavior

### Patch Application
1. Current hunk is extracted from the visible diff
2. Temporary patch file is created with just that hunk
3. Patch is applied to the **first file** in the diff using `patch` command
4. If successful, hunk is marked as applied
5. Diff is regenerated to show updated state

### File Management
- **Temp Files**: Only create temporary files for the target being modified
- **Original Files**: Never modify original files directly  
- **Patch Files**: Generate `.patch` files in same directory as target files when exiting
- **Multiple Directions**: A single hunk session can modify files in both directions, resulting in patches for both LEFT and RIGHT files

### Patch Generation Logic
When exiting hunk mode, system checks which files were modified:
- If LEFT file modified: Generate `left_file.patch` (diff from original-left to temp-left)
- If RIGHT file modified: Generate `right_file.patch` (diff from original-right to temp-right)  
- If BOTH modified: Generate both patch files
- **Implementation Note**: Current logic needs verification to handle both-files-modified case

### Safety Features
- **No Hidden Transformations**: Patch content exactly matches what user sees
- **Direction Explicit**: User must explicitly choose direction with `r` key
- **Failure Handling**: If patch fails, show error and don't mark hunk as applied

## User Interface

### Key Bindings
| Key | Action | Context |
|-----|--------|---------|
| `SPACE` | Enter hunk mode | Diff view |
| `n` | Next hunk | Hunk mode |
| `p` | Previous hunk | Hunk mode |
| `<` | Apply current hunk | Hunk mode |
| `r` | Toggle diff direction | Hunk mode |
| `ESC` | Exit hunk mode | Hunk mode |

### Display Elements
```
Hunk Mode: src/main.go (LEFT → RIGHT) (Hunk 2 of 5)

@@ -10,3 +10,4 @@
 func main() {
-    fmt.Println("old")
+    fmt.Println("new")
+    fmt.Println("added")
 }

n/p: next/prev hunk  <: apply hunk  r: reverse diff  ESC: exit hunk mode
Applied: 1 hunks
```

## Overall Workflow Context

### Hunk Mode vs Apply Phase
**Hunk Mode** is part of a larger workflow:
1. **Interactive Review**: User selectively applies individual hunks in either direction
2. **Patch Generation**: System generates `.patch` files for all modified files  
3. **Apply Phase**: Later, patch files are applied to actual source files

### Multiple Direction Support
Even though each individual hunk action goes in a single direction (`<` applies visible patch), a complete hunk session can result in changes to **both LEFT and RIGHT files**:

- User applies some hunks LEFT→RIGHT (modifying RIGHT file via normal diff)
- User presses `r` to reverse, then applies other hunks RIGHT→LEFT (modifying LEFT file via reversed diff)  
- **Final Result**: Both files have changes
- **Patch Output**: Generates both `left_file.patch` and `right_file.patch` 
- **Apply Phase**: Both patch files are available for later application to source files

This design allows maximum flexibility while keeping individual hunk actions simple and predictable.

## Example Scenarios

### Scenario 1: Cherry-pick Changes Left to Right
1. User has `old.txt` (left) and `new.txt` (right)
2. Wants to selectively copy some changes from new.txt to old.txt
3. **Default view**: `diff -u old.txt new.txt` 
4. Press `<` on desired hunks → modifies `old.txt`
5. Result: `old.txt` gets selected changes from `new.txt`

### Scenario 2: Revert Some Changes
1. User has `original.txt` (left) and `modified.txt` (right)  
2. Wants to revert some changes in modified.txt back to original
3. Press `r` to reverse: `diff -u modified.txt original.txt`
4. Press `<` on desired hunks → modifies `modified.txt`
5. Result: `modified.txt` gets selected parts reverted to `original.txt`

### Scenario 3: Bidirectional Merge (Multiple Directions)
1. User has `version_a.txt` (left) and `version_b.txt` (right)
2. Wants to create a merged version with changes from both files
3. **Step 1**: Default view `diff -u version_a.txt version_b.txt`
   - Press `<` on some hunks → modifies `version_a.txt` (gets features from B)
4. **Step 2**: Press `r` to reverse: `diff -u version_b.txt version_a.txt`  
   - Press `<` on other hunks → modifies `version_b.txt` (gets features from A)
5. **Final Result**: 
   - Both files modified
   - Generates `version_a.txt.patch` and `version_b.txt.patch`
   - Apply phase can use both patches on source files

## Design Rationale

### Why Only `<` Action?
- **Eliminates Confusion**: No need to remember direction mappings
- **WYSIWYG Principle**: What you see is what you apply
- **User Controls Direction**: `r` key gives explicit control

### Why Refresh on Direction Change?
- **Visual Consistency**: User always sees the actual diff being applied
- **No Hidden Logic**: No behind-the-scenes transformations
- **Predictable Results**: Apply operation matches visible content exactly

### Why First File Rule?
- **Standard Patch Behavior**: Matches how `patch` command works with `diff -u`
- **Intuitive**: Patch describes "transform first file to match second file"
- **Consistent**: Same logic regardless of which files are involved

## Questions for Review

1. **Is the single-action approach (`<` only) intuitive enough?**
2. **Is the direction control (`r` to reverse) clear to users?**
3. **Should we consider visual indicators for which file is the target?**
4. **Are the key bindings logical and memorable?**
5. **Is the "first file gets modified" rule intuitive?**
6. **Should we show a preview of what will change before applying?**

## Alternative Designs Considered

### Two-Action Approach (Rejected)
- `>`: Apply left-to-right
- `<`: Apply right-to-left
- **Problem**: Risk of hunk mismatch when computing reverse diffs
- **Problem**: Confusion about which hunks correspond to which actions

### Target File Selection (Not Implemented)
- Let user explicitly choose which file to modify
- **Downside**: Adds complexity for little benefit
- **Current approach**: Direction control achieves same result more intuitively
