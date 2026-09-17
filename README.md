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
archery mydb -c 'SELECT * FROM users LIMIT 10' --csv > users.csv
```

Queries are read-only — Archery blocks DML/DDL on the query endpoint. Changing data
goes through its review workflow instead, which `archery workflow` drives from the
same shell (see [Submit and review SQL workflows](#submit-and-review-sql-workflows)).

## Install

**Homebrew (macOS / Linux):**

```bash
brew install rjchien728/tap/archery
```

**Go:**

```bash
go install github.com/rjchien728/archery-cli/cmd/archery@latest
```

Or grab a binary from [Releases](https://github.com/rjchien728/archery-cli/releases).

## Configure

Set these environment variables (typically in your shell profile or a `.env`):

| Variable | Required | Description |
|----------|----------|-------------|
| `ARCHERY_URL` | yes | Base URL, e.g. `https://archery.example.com` |
| `ARCHERY_INSTANCE` | for queries | The database server to talk to. Archery calls each registered server an *instance*; you will find its name in the instance dropdown on Archery's SQL query or SQL workflow page. |
| `ARCHERY_USERNAME` | yes | Login username |
| `ARCHERY_PASSWORD` | when logging in | Login password. If unset, `archery` prompts on `/dev/tty` (works even when stdin is piped), and only when it actually has to log in — a cached session never prompts. Set it in non-interactive contexts (CI, containers). |
| `ARCHERY_ALIASES` | no | Comma-separated `short=full` pairs, e.g. `prod=db_orders_prod,stg=db_orders_stg` |
| `ARCHERY_INSECURE` | no | `1`/`true` to skip TLS certificate verification (unsafe — MITM risk) |
| `ARCHERY_CACERT` | no | Path to a PEM file with extra trusted CA certificates (for internal/private CAs) |

Flags override env: `--endpoint`, `--instance`, `--username`, `--insecure`/`-k`, `--cacert`. There is no `--password` flag by design — credentials never go through argv where they'd appear in `ps` and shell history.

The first time you run `archery`, it logs in via Archery's standard Django session flow and caches cookies at `~/.cache/archery/cookies.json` (mode `0600`). Subsequent calls reuse the session; if it expires, the CLI re-logs in transparently.

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

### Submit and review SQL workflows

Queries are read-only. Writes go through archery's review flow instead: submit a
workflow, approve it, then execute it. Each command maps to one action in archery's
web UI — chaining them is left to you.

Audit a statement first. `check` creates nothing, and exits non-zero when archery
rejects the statement, so it works as a gate:

```console
$ archery workflow check --instance mysql-staging -d mydb \
    -c "UPDATE users SET status = 'active' WHERE id = 42;"
 id | stage_status    | affected_rows | actual_affected_rows | execute_time | error_message | sql                                              
----+-----------------+---------------+----------------------+--------------+---------------+--------------------------------------------------
 1  | Audit completed | 1             |                      | 0            | None          | UPDATE users SET status = 'active' WHERE id = 42 
(1 row)
```

Submitting prints the workflow id you drive everything else with:

```console
$ archery workflow submit --instance mysql-staging -d mydb \
    --name 'reactivate user 42' -c "UPDATE users SET status = 'active' WHERE id = 42;"
 workflow_id | status                | group | db   | audit_auth_groups 
-------------+-----------------------+-------+------+-------------------
 900         | workflow_manreviewing | dba   | mydb | dba-review        
(1 row)
```

Approving does not run it — execution is a separate step, as in the UI:

```console
$ archery workflow approve 900 --remark 'checked'
 workflow_id | action  
-------------+---------
 900         | approve 
(1 row)

$ archery workflow execute 900
 workflow_id | action  
-------------+---------
 900         | execute 
(1 row)

$ archery workflow status 900
 workflow_id | status          
-------------+-----------------
 900         | workflow_finish 
(1 row)
```

`show` reports what each statement actually did, `log` who did what:

```console
$ archery workflow show 900
 id | stage_status         | affected_rows | actual_affected_rows | execute_time | error_message | sql                                              
----+----------------------+---------------+----------------------+--------------+---------------+--------------------------------------------------
 1  | Execute Successfully | 1             | 1                    | 0.0031       | None          | UPDATE users SET status = 'active' WHERE id = 42 
(1 row)

$ archery workflow log 900
 operation_time      | operation_type | operator | operation_info                 
---------------------+----------------+----------+--------------------------------
 2026-09-08 11:42:08 | execute        | alice    | finished normally              
 2026-09-08 11:41:55 | approve        | alice    | remark: checked                
 2026-09-08 10:15:03 | submit         | roger    | waiting for review: dba-review 
(3 rows)
```

Rejecting someone else's workflow, or abandoning your own, is `cancel`. Archery's
web UI asks for a reason before enabling the button, but the endpoint accepts a
blank one, so `--remark` is optional here:

```console
$ archery workflow list --status workflow_manreviewing
 id  | workflow_name    | status                | engineer | group | instance      | db   | create_time         
-----+------------------+-----------------------+----------+-------+---------------+------+---------------------
 214 | drop stale index | workflow_manreviewing | alice    | dba   | mysql-staging | mydb | 2026-09-08 09:30:12 
(1 row)

$ archery workflow cancel 214 --remark 'wrong target database'
 workflow_id | action 
-------------+--------
 214         | cancel 
(1 row)
```

`--instance` and `--group` accept a name or a numeric id. A *group* is how archery
partitions permissions — each instance belongs to one, and you will find both names
in the dropdowns on its SQL workflow page. Given a name, archery-cli finds the group
that holds the instance, so `--group` is only needed when an instance appears in
several of them; passing ids skips the lookup entirely.

What you may approve is decided by archery, not by this CLI: if your account is not
in the workflow's audit group, `approve` fails with archery's own message.

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

```
archery workflow check    -d <db> ( -c <sql> | -f <file> )
                          [--instance <name|id>] [--group <name|id>]
archery workflow submit   -d <db> --name <title> ( -c <sql> | -f <file> )
                          [--instance <name|id>] [--group <name|id>]
                          [--backup] [--demand-url <url>]
                          [--run-date-start <ts>] [--run-date-end <ts>]
archery workflow list     [--instance <name|id>] [--group <name|id>]
                          [--status <s>] [--syntax-type <n>]   # 1=DDL, 2=DML
                          [--search <q>] [--since <date>] [--until <date>]
                          [--limit <n>] [--offset <n>]
archery workflow show     <workflow-id>
archery workflow log      <workflow-id>
archery workflow approve  <workflow-id> [--remark <text>]
archery workflow cancel   <workflow-id> [--remark <text>]
archery workflow execute  <workflow-id> [--mode auto|manual]
archery workflow status   <workflow-id>

Shared by every workflow subcommand:
  [--endpoint <url>] [--username <name>] [--insecure | -k] [--cacert <file>]
  [--csv | --json | -x]  [--max-col-width <n>]  [-v]

The commands addressed by a workflow id take no --instance: the workflow already
knows where it runs.
```

A database literally named `workflow` has to be passed as `-d workflow`, since the
bare word selects the subcommand.

`workflow list --limit` pages the workflow list. It is unrelated to the root
command's `-L/--limit`, which caps the rows a query returns.

## Proxy

`archery` honours `HTTPS_PROXY` / `HTTP_PROXY` and supports SOCKS5 (`socks5://` and `socks5h://`).

## License

MIT
