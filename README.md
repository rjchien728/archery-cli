# archery-cli

[![CI](https://github.com/rjchien728/archery-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/rjchien728/archery-cli/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/rjchien728/archery-cli.svg)](https://pkg.go.dev/github.com/rjchien728/archery-cli)
[![Release](https://img.shields.io/github/v/release/rjchien728/archery-cli)](https://github.com/rjchien728/archery-cli/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A `psql`-style command line client for [Archery](https://github.com/hhyo/Archery) — the SQL audit and query platform.

`archery` lets you run read-only SQL queries against any database that's exposed through an Archery instance, from the comfort of your terminal. It also exposes a handful of `psql`-style meta commands (`\l`, `\dt`, `\d`, `\dn`) for browsing schema metadata.

## Why

Archery's web UI is great for ad-hoc one-off queries, but painful for anything you want to script, pipe, diff, or feed to an LLM. This CLI wraps Archery's HTTP API behind a familiar interface so you can:

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
| `ARCHERY_PASSWORD` | yes | Login password |
| `ARCHERY_ALIASES` | no | Comma-separated `short=full` pairs, e.g. `prod=db_orders_prod,stg=db_orders_stg` |

Flags override env: `--endpoint`, `--instance`, `--username`, `--password`.

The first time you run `archery`, it logs in via Archery's standard Django session flow and caches cookies at `~/.cache/archery/cookies.txt` (mode `0600`). Subsequent calls reuse the session; if it expires, the CLI re-logs in transparently.

## Usage

```
archery [<db> | -d <db>]
        ( -c <sql> | -f <file> | < stdin )
        [--csv | --json | -x]
        [-L <limit>]            # default 100
        [--schema <name>]       # default 'public'
        [--max-col-width <n>]   # default 60
        [-v]                    # verbose to stderr

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
