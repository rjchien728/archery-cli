---
name: archery
description: Run read-only SQL queries against databases exposed through Archery. Use when the user asks about DB content, row counts, table structure, or anything that requires looking up live data.
---

# Archery query skill

When the user asks a question that requires looking up data in a database managed by Archery, use the `archery` CLI.

## How to run queries

- `archery <db> -c '<SQL>'` — runs a SELECT against `<db>`. `<db>` is either a configured alias or a full database name.
- Only `SELECT` is supported. Archery blocks DML/DDL on the query endpoint — do not attempt INSERT / UPDATE / DELETE / DDL.
- For machine-parseable output use `--csv` or `--json`. For rows with many columns use `-x` (expanded display).

## Schema discovery

- `archery <db> -c '\l'`           — list databases
- `archery <db> -c '\dt'`          — list tables in the current schema
- `archery <db> -c '\d <table>'`   — describe a table (columns)
- `archery <db> -c '\dn'`          — list schemas

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
