@AGENTS.md

## Claude Code

- Use plan mode for changes to `internal/render/` or `internal/check/`: the
  first emits resources a cluster applies, the second is what stops a broken
  board reaching it.
- Prefer `make` targets over ad-hoc `go` commands, so what you run is what CI
  runs.
- After changing anything that renders, run `make generate` and include the
  result. A change that leaves `generated/` stale fails CI, not review.
- When a conformance rule blocks a change, fix the resource. Do not edit
  `internal/check` or `.golangci.yml` to make a finding go away without saying
  so explicitly and explaining why the rule is wrong.
