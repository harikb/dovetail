# Why Dovetail?

## The Problem
Working over SSH or in headless environments, GUI diff tools (`meld`, `kdiff3`, etc.) are unusable. Terminal-based tools like `vimdiff` only work on individual file pairs, not directory trees.

## What Dovetail Provides

**Pure terminal operation**: No X11, GUI, or windowing system dependencies. Works over SSH, in containers, on servers.

**Directory tree comparison**: Interactive TUI for comparing entire directory structures, not just individual files.

## Use Cases

- Comparing configurations across servers (over SSH)
- Directory synchronization in headless environments  
- Working with multiple separate repositories/codebases
- Any situation where GUI tools can't be used

That's it. If you have a windowing system available, use `meld`. If you're comparing single files, use `vimdiff`. If you need terminal-based directory comparison, use Dovetail.
