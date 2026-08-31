---
paths:
  - "internal/dashboards/**/*.go"
  - "internal/alerts/**/*.go"
  - "internal/panels/**/*.go"
  - "internal/common/**/*.go"
  - "internal/registry/**/*.go"
---

# Authoring boards and alert groups

## The shape of a resource file

Data plus a `Build` function. No methods — `registry.Dashboard` and
`registry.AlertGroup` satisfy `Resource` on your behalf, which is what keeps
fifty resource files free of ten lines of boilerplate each.

    func init() {
        registry.Add(registry.Dashboard{
            Meta: registry.Meta{
                UID:     uid,
                Title:   "Envoy — Edge",
                Folder:  folders.APIGateway,
                Owner:   "platform",
                Publish: true,
            },
            Build: build,
        })
    }

`Owner` is required. At fifty boards the expensive question is "whose is this",
and the answer belongs in the code rather than in someone's memory.

`Publish: false` still registers the resource, so it is still covered by every
conformance check — it is just not written to `generated/`. That is how the
example alert group exercises the SDK-to-CRD translation without paging anyone.

## Start from common.NewDashboard

It applies the refresh interval, time range, timezone, tooltip mode, tags, the
datasource variable, and the link back to source. A board built any other way is
missing the datasource variable, and every panel on it renders empty with no
error anywhere.

## Layout

`.Span(n)` where n is out of 24, and `.Height(n)`. The SDK computes `gridPos`.
Never set `gridPos` — the repo this replaces has a "Fix grid overlaps" commit,
and `check.RuleGridOverlap` exists so that cannot happen again.

Order panels by what an on-call engineer opens first. Rate, errors and duration
before the runtime internals; a board where the useful panel is below the fold
is a board people screenshot instead of using.

## Descriptions earn their place

Put in a panel `Description` what a reader cannot infer: why 4xx is excluded,
what a unit means, which of two similar metrics this is. Do not restate the
title.

## Alert rules

Two queries: the metric in refId `A`, a threshold expression on `__expr__` in
refId `B`, and `Condition("B")`. A condition naming a refId no query produces
evaluates to nothing and reports no error, so the rule never fires —
`render.AlertRuleGroup` rejects that rather than letting it ship.

The SDK requires `.RuleGroup(...)` on each rule and the CRD rejects the field;
set it and let `internal/render` drop it.

`NoDataState(NoData)`, not `Alerting`. A service with no traffic has no error
ratio, and paging someone because a queue is quiet teaches them to ignore the
alert.

Every rule needs a `severity` label and `summary`, `description` and
`runbook_url` annotations. An alert without a runbook is an alert someone will
acknowledge without acting on.
