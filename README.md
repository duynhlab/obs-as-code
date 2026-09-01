# obs-as-code

Grafana dashboards for the duynhlab platform, written in Go with the
[Grafana Foundation SDK](https://github.com/grafana/grafana-foundation-sdk).

The output is **plain dashboard JSON** — the same thing you would export from the
UI, importable into any Grafana. How it reaches a cluster is deliberately not
this repository's business; see [`docs/flux/`](docs/flux/README.md) for how
homelab does it.

    Go  →  make generate  →  dashboard JSON  →  tag  →  OCI artifact  →  grafana-operator  →  Grafana

## Why not JSON

The repo this replaces held 21 hand-maintained boards, and the drift was
measurable: the README listed five files that no longer existed and missed four
that did, `schemaVersion` spanned 27 to 41, four panels hardcoded a datasource
UID, and two boards shared a title. Its history contains a "Fix grid overlaps"
commit and a "Fix duplicate refId" commit — both classes of bug a compiler and
one table-driven test catch for free. A 293 KB, 74-panel JSON diff is not
something a reviewer can read.

Here, a board is Go: the compiler checks the schema, the SDK computes panel
layout, one function owns every house default, and a conformance suite runs
twelve rules over every board on every commit — including a PromQL parse through
both the VictoriaMetrics and the Prometheus parsers, so a query is both valid
against the engine that runs it and portable to a plain Prometheus.

## Getting started

    make check      # everything CI runs
    make preview    # look at the boards in a local Grafana
    make help       # every target

Go 1.26.7 — `make` pins `GOTOOLCHAIN` to it, so a newer local Go still produces
the output CI expects. Docker is needed for `make preview`; nothing else.

## Adding a dashboard

Copy `internal/dashboards/example/obs-as-code-example.go`, add one import to
`internal/catalog/catalog.go`, then `make golden && make check`.

The full contract is in [`AGENTS.md`](AGENTS.md) — written for coding agents, and
the shortest accurate description of the conventions for humans too.
[`docs/stack-facts.md`](docs/stack-facts.md) has the cluster reference.

## Status

Two boards so far: the permanent example that keeps the conformance suite
exercised, and the Kubernetes cluster overview. The rest still live in
`duynhlab/grafana-dashboards` and are ported one at a time.
