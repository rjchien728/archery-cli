---
name: archery
description: Run read-only SQL queries against databases exposed through Archery, and drive Archery's SQL review workflow (submit, approve, execute) when a write is needed. Use when the user asks about DB content, row counts, table structure, anything that requires looking up live data, or asks to change data through Archery.
---

# Archery query skill

When the user asks a question that requires looking up data in a database managed by Archery, use the `archery` CLI.

## How to run queries

- `archery <db> -c '<SQL>'` — runs a SELECT against `<db>`. `<db>` is either a configured alias or a full database name.
- Only `SELECT` is supported here. Archery blocks DML/DDL on the query endpoint — do not attempt INSERT / UPDATE / DELETE / DDL through `-c`. Writes go through the workflow commands below.
- For machine-parseable output use `--csv` or `--json`. For rows with many columns use `-x` (expanded display).

## Schema discovery

- `archery <db> -c '\l'`           — list databases
- `archery <db> -c '\dt'`          — list tables in the current schema
- `archery <db> -c '\d <table>'`   — describe a table (columns)
- `archery <db> -c '\dn'`          — list schemas

## Writing data: SQL workflows

Archery does not let you write through the query endpoint. A write is a workflow:
submit it, get it approved, then execute it. Each step is its own command, and
approving does **not** execute.

```bash
archery workflow check   --instance <inst> -d <db> -c '<SQL>'   # audit only, creates nothing
archery workflow submit  --instance <inst> -d <db> --name '<title>' -c '<SQL>'
archery workflow approve <id> [--remark '<note>']
archery workflow execute <id>
archery workflow status  <id>                                   # workflow_finish when done
archery workflow show    <id>                                   # per-statement result
```

Reading what is in flight: `archery workflow list [--status workflow_manreviewing]`,
and `archery workflow log <id>` for a workflow's audit trail.

Rules for this skill:

- **Always pass `--instance` explicitly** on every workflow command. Without it the
  CLI falls back to `ARCHERY_INSTANCE`, which is often a production instance — enough
  to file a real production workflow that the user's colleagues can see and approve.
- **Always run `check` first** and show the user the audit result before submitting. It
  reports the statement archery parsed and how many rows it expects to touch.
- **Get the user's agreement before `submit`, `approve`, `execute` or `cancel`.** These
  change shared state, are visible to the user's colleagues, and executing cannot be
  undone from here.
- Quote the workflow id back to the user after submitting, so they can follow it in
  Archery's web UI.
- `cancel` takes `--remark`. Archery accepts a blank reason, but always write one: it is what the next person sees when they wonder why the workflow died.
- Whether the user may approve a given workflow is Archery's decision. If `approve`
  fails with a permission or state message, relay it — do not try another route.

## Prerequisites

The user must have these env vars set before `archery` can authenticate:

- `ARCHERY_URL`
- `ARCHERY_INSTANCE`
- `ARCHERY_USERNAME`
- `ARCHERY_PASSWORD`

If a command fails with an authentication error, stop and ask the user to configure the above — do not guess credentials or endpoints.

If `ARCHERY_PASSWORD` is not set, `archery` prompts for it on `/dev/tty`. In AI tool contexts that don't forward `/dev/tty` you'll see either an apparent "hang" (the user is being prompted on their terminal but you can't see it) or `no terminal available for password prompt`. In both cases, stop and ask the user to `export ARCHERY_PASSWORD=...` before retrying — do not attempt to supply a password yourself.

If a command fails with a TLS certificate error, stop and tell the user. They can configure `ARCHERY_CACERT=<path-to-pem>` (preferred, for private CAs) or `ARCHERY_INSECURE=1` (last resort, unsafe). Do not add `--insecure` or `--cacert` to queries yourself — these are user-owned trust decisions.

## When to use this skill

Proactively reach for `archery` whenever the user's question implies looking at real data ("how many X", "which rows", "what's in table Y", "give me the schema of Z"). If the user has multiple databases configured and the question is ambiguous about which one, ask before querying.
