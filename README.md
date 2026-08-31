# obs-as-code

Grafana dashboards and alert rules for the duynhlab platform, written in Go with
the [Grafana Foundation SDK](https://github.com/grafana/grafana-foundation-sdk)
and rendered into Grafana Operator custom resources.

    Go  →  make generate  →  generated/  →  tag  →  OCI artifact  →  Flux  →  grafana-operator  →  Grafana

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
the output CI expects. `kustomize` and `kubeconform` are needed for
`make validate`, and Docker for `make preview`.

## Adding a dashboard

Copy `internal/dashboards/example/obs-as-code-example.go`, add one import to
`internal/catalog/catalog.go`, then `make golden && make check`.

The full contract is in [`AGENTS.md`](AGENTS.md) — written for coding agents, and
the shortest accurate description of the conventions for humans too.
[`docs/stack-facts.md`](docs/stack-facts.md) has the cluster reference.

## Status

Phase 1: the framework, tooling, tests and CI, plus one published example board
that exists to prove the whole delivery path. Porting real boards is the next
wave; the boards still live in `duynhlab/grafana-dashboards` until then.

Delivery to the cluster needs two manifests copied into homelab — see
[`flux/README.md`](flux/README.md), which also records the one open question:
this repository is private, so its GHCR package is too, and Flux needs either a
pull secret or a public package.
