# archery-cli

[![CI](https://github.com/rjchien728/archery-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/rjchien728/archery-cli/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/rjchien728/archery-cli.svg)](https://pkg.go.dev/github.com/rjchien728/archery-cli)
[![Release](https://img.shields.io/github/v/release/rjchien728/archery-cli)](https://github.com/rjchien728/archery-cli/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A `psql`-style command line client for [Archery](https://github.com/hhyo/Archery) — and the simplest way to let AI tools (Claude Code, Cursor, ChatGPT) query your databases directly, without copy-pasting from Archery's web UI.

`archery` lets you run read-only SQL queries against any database that's exposed through an Archery instance, from the comfort of your terminal. It also exposes a handful of `psql`-style meta commands (`\l`, `\dt`, `\d`, `\dn`) for browsing schema metadata.

## Why

Archery's web UI is great for ad-hoc one-off queries, but painful for anything you want to script, pipe, diff, or hand to an AI. This CLI wraps Archery's HTTP API behind a familiar interface so both you and your AI tools can query your databases from the shell — replacing the "open Archery → copy rows → paste into ChatGPT" loop:

```bash
archery mydb -c 'SELECT count(*) FROM orders WHERE created_at > now() - interval 7 day'
archery mydb -c 'SELECT * FROM users LIMIT 10' --csv > users.csv
echo 'SELECT version()' | archery mydb
archery mydb -c '\dt'
archery mydb -c '\d orders'
```

Only `SELECT` is supported (Archery itself blocks DML/DDL on the `/query/` endpoint).

## Install

```bash
go install github.com/rjchien728/archery-cli/cmd/archery@latest
```

Or grab a binary from [Releases](https://github.com/rjchien728/archery-cli/releases).

## Configure

Set these environment variables (typically in your shell profile or a `.env`):

| Variable | Required | Description |
|----------|----------|-------------|
| `ARCHERY_URL` | yes | Base URL, e.g. `https://archery.example.com` |
| `ARCHERY_INSTANCE` | yes | Instance name as configured in Archery |
| `ARCHERY_USERNAME` | yes | Login username |
| `ARCHERY_PASSWORD` | yes* | Login password. *If unset, `archery` prompts on `/dev/tty` (works even when stdin is piped). Required when running in non-interactive contexts (CI, containers). |
| `ARCHERY_ALIASES` | no | Comma-separated `short=full` pairs, e.g. `prod=db_orders_prod,stg=db_orders_stg` |
| `ARCHERY_INSECURE` | no | `1`/`true` to skip TLS certificate verification (unsafe — MITM risk) |
| `ARCHERY_CACERT` | no | Path to a PEM file with extra trusted CA certificates (for internal/private CAs) |

Flags override env: `--endpoint`, `--instance`, `--username`, `--insecure`/`-k`, `--cacert`. There is no `--password` flag by design — credentials never go through argv where they'd appear in `ps` and shell history.

The first time you run `archery`, it logs in via Archery's standard Django session flow and caches cookies at `~/.cache/archery/cookies.json` (mode `0600`). Subsequent calls reuse the session; if it expires, the CLI re-logs in transparently.

---

👉 **Want your AI to query your DB?** See [AI Integration](#ai-integration) — drop in one skill file and you're done.
👉 **Want to run queries yourself from the terminal?** See [Manual Usage](#manual-usage).

---

## AI Integration

Once the `archery` skill is installed, your AI tool can answer questions like *"how many orders did we get last week in prod?"* by running the right `archery` command itself, no copy-paste required.

**Install the skill (Claude Code):**

```bash
curl -O https://raw.githubusercontent.com/rjchien728/archery-cli/main/skills/archery.skill.md
mv archery.skill.md ~/.claude/skills/
```

**Example interaction:**

> **You:** How many orders came in last week in prod?
>
> **Claude:** *runs* `archery prod -c 'SELECT count(*) FROM orders WHERE created_at > now() - interval 7 day'`
>
> **Claude:** 12,483 orders last week.

**Other AI tools (Cursor, Windsurf, ChatGPT, …):** the skill file is plain Markdown with YAML frontmatter. Paste the contents of `archery.skill.md` into your tool's system prompt / custom instructions.

## Manual Usage

### Run a query

```bash
archery mydb -c 'SELECT * FROM users WHERE id = 42'
```

Output is a `psql`-style aligned table by default.

### Export as CSV or JSON

```bash
archery mydb -c 'SELECT * FROM users LIMIT 10' --csv > users.csv
archery mydb -c 'SELECT count(*) FROM orders' --json | jq '.rows[0]'
```

### Run a query from a file or stdin

```bash
archery mydb -f reports/daily.sql
echo 'SELECT version()' | archery mydb
```

### Wide rows (expanded display)

For rows with many columns, `-x` prints one column per line:

```bash
archery mydb -c 'SELECT * FROM users LIMIT 1' -x
```

### Browse schema

```bash
archery mydb -c '\l'          # list databases
archery mydb -c '\dt'         # list tables in the current schema
archery mydb -c '\d orders'   # describe table
archery mydb -c '\dn'         # list schemas
```

### Use aliases

Map short names to full database names via `ARCHERY_ALIASES`:

```bash
export ARCHERY_ALIASES=prod=db_orders_prod,stg=db_orders_stg
archery prod -c 'SELECT count(*) FROM orders'
```

## Reference

```
archery [<db> | -d <db>]
        ( -c <sql> | -f <file> | < stdin )
        [--csv | --json | -x]
        [-L <limit>]            # default 100
        [--schema <name>]       # default 'public'
        [--max-col-width <n>]   # default 60
        [-v]                    # verbose to stderr
        [--insecure | -k]       # skip TLS verification (unsafe)
        [--cacert <file>]       # trust additional CA certificates

Meta commands (passed via -c):
  \l            list databases
  \dn           list schemas
  \dt           list tables in current schema
  \d <table>    describe a table (columns)
  \?            print this help
```

`<db>` may be either a configured alias or a full database name; aliases are resolved transparently.

## Proxy

`archery` honours `HTTPS_PROXY` / `HTTP_PROXY` and supports SOCKS5 (`socks5://` and `socks5h://`).

## License

MIT
