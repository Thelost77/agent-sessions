# agent-sessions

`agent-sessions` searches local coding-agent sessions from one command.

It supports [Pi](https://github.com/badlogic/pi-mono), [Codex](https://github.com/openai/codex), [OpenCode](https://github.com/anomalyco/opencode), and [Claude Code](https://github.com/anthropics/claude-code).

## Features

- Search all supported harnesses through one local index.
- Find exact terms and phrases with SQLite FTS5 and BM25.
- Find misspelled terms with character n-grams.
- Find related text with local Ollama embeddings.
- Combine search channels into one result per session.
- Filter by harness, project path, role, and date.
- Print native session IDs and resume commands.
- Produce JSON output for scripts and other tools.
- Update only new or changed sessions during indexing.
- Keep session text on the local machine.

## Requirements

- Go 1.25 or later
- [Ollama](https://ollama.com/) for semantic search
- The `all-minilm` Ollama model for the default configuration

Install the default embedding model:

```sh
ollama pull all-minilm
```

Lexical and fuzzy search work without Ollama. A later indexing run adds missing embeddings.

## Installation

Install the latest release with Go:

```sh
go install github.com/Thelost77/agent-sessions/cmd/agent-sessions@latest
```

Or build and install from source:

```sh
git clone https://github.com/Thelost77/agent-sessions.git
cd agent-sessions
make test
make install
```

`make install` installs to `~/.local/bin` by default. Set `PREFIX` or `INSTALL_DIR` to use another path.

## Usage

Build or update the index:

```sh
agent-sessions index
```

Search all indexed sessions:

```sh
agent-sessions search "generate QR codes without assets"
```

Restrict the search to one project tree:

```sh
agent-sessions search \
  --path ~/projects/qr-codes \
  "generate QR codes without assets"
```

Apply more filters:

```sh
agent-sessions search \
  --harness pi,codex,opencode \
  --role user,assistant \
  --since 2026-07-01 \
  --before 2026-08-01 \
  --limit 20 \
  "empty state"
```

Show the rank from each search channel:

```sh
agent-sessions search --explain "genrate qr cdoes emtpy state"
```

Disable semantic search:

```sh
agent-sessions search --lexical-only "empty state"
```

Produce JSON output:

```sh
agent-sessions search --json "empty state"
```

Inspect the index and its dependencies:

```sh
agent-sessions status
agent-sessions doctor
```

Put all flags before the query. The CLI uses Go standard flag syntax.

## Commands

### `index`

`index` supports these main options:

```text
--rebuild             Recreate the complete index
--reembed             Clear and regenerate all embeddings
--harness             Index selected harnesses
--quiet               Hide progress output
--index                Set the index path
--embedding-url        Set the Ollama URL
--embedding-model      Set the embedding model
```

The indexer preserves embeddings when stable chunk hashes do not change. It reads the OpenCode database in read-only mode.

The JSONL adapters skip each malformed line and continue parsing the remaining session. `doctor` reports all saved parser warnings.

### `search`

Each result contains:

- the harness and session ID;
- the session title and directory;
- the best matching entry and excerpt;
- a shell-quoted native resume command;
- optional channel ranks with `--explain`.

The command prints resume commands but does not run them.

### `status`

`status` reports source, session, chunk, embedding, pending-vector, and parser-warning counts.

### `doctor`

`doctor` checks source discovery, OpenCode compatibility, index access, Ollama access, and embedding dimensions.

## Configuration

The optional configuration file is `~/.config/agent-sessions/config.toml`.

```toml
index = "~/.local/share/agent-sessions/index.sqlite"

[embedding]
url = "http://127.0.0.1:11434"
model = "all-minilm"

[harnesses]
pi = true
codex = true
opencode = true
claude = true

[sources]
pi = "~/.pi/agent/sessions"
codex = "~/.codex/sessions"
claude = "~/.claude/projects"
# Leave this empty to use `opencode db path`.
opencode = ""
```

Set `AGENT_SESSIONS_CONFIG` to use a different file. Command flags override configuration values.

The embedding URL must use a loopback address. This rule prevents accidental uploads of private session text.

## Source Data

The default adapters read these stores:

| Harness | Default source |
|---|---|
| Pi | `~/.pi/agent/sessions/**/*.jsonl` |
| Codex | `~/.codex/sessions/**/*.jsonl` |
| OpenCode | The database returned by `opencode db path` |
| Claude Code | `~/.claude/projects/**/*.jsonl` |

The index includes user messages, assistant prose, compaction summaries, and useful session metadata.

The index excludes tool output, reasoning, patches, snapshots, base64 data, and duplicate protocol events.

## Storage and Privacy

The default index is `~/.local/share/agent-sessions/index.sqlite`.

The data directory uses mode `0700`. The index and lock file use mode `0600`.

The index contains derived copies of private session text. Delete the SQLite, WAL, and SHM files to remove this data.

The tool does not use a search server or a cloud embedding service. Ollama requests stay on the local machine.

## Scheduling

Run manual indexing first. Example systemd user units are available in [`contrib/systemd`](contrib/systemd).

Install the units:

```sh
mkdir -p ~/.config/systemd/user
cp contrib/systemd/agent-sessions-index.* ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now agent-sessions-index.timer
```

The timer does not start Ollama. Lexical indexing still succeeds when Ollama is unavailable.

## Releases

Use SemVer tags with a leading `v`. Keep release notes in `docs/releases/`.

```sh
${EDITOR:-vi} docs/releases/v0.2.0.md
./scripts/release.sh v0.2.0
```

The release script creates the tag, pushes it, and publishes the GitHub Release.

## Development

Run all checks:

```sh
make check
```

Build the executable:

```sh
make build
```

Automated tests use sanitized fixtures and temporary SQLite databases. They do not require Ollama.

## License

[MIT](LICENSE)
