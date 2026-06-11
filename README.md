# modux

Model-optimizing wrapper for Claude Code and Codex CLI

A proxy tool that automatically runs Claude and Codex commands with optimal model selection.

## Installation

```bash
go build -o modux
```

After building, place the `modux` binary in a directory on your PATH:

```bash
sudo mv modux /usr/local/bin/
```

## Configuration

Place the configuration file at:

```
~/.config/modux/config.toml
```

## Usage

```bash
# Run Claude Code
modux claude [args...]

# Run Codex CLI
modux codex [args...]
```

Arguments are passed through to the underlying CLI unchanged.

Examples:
```bash
modux claude --help
modux codex agent run my-agent
```

## Authentication

Uses existing Claude Code / Codex CLI authentication. No separate API key is required.
