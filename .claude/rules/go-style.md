---
paths:
  - "**/*.go"
---

# Go conventions

Matches `duynhlab/pkg`, so moving between the two repos does not mean switching
habits.

- **Standard library `testing`.** No testify, no go-cmp. `pkg` carries testify
  only as an indirect dependency, and an assertion library is not worth a direct
  one here.
- **Table-driven tests with named subtests**, and `t.Parallel()` on anything
  independent. The subtest name is what a CI log shows, so name it after the
  case, not `case 1`.
- **Test files are named after source files**: `render/gzip.go` →
  `render/gzip_test.go`. Not after the function under test.
- **External test packages** (`package foo_test`) unless the test genuinely
  needs an unexported symbol. It keeps the test honest about what the package
  actually offers.
- **Errors wrap with `%w`** and name the subject: `render dashboard %q: ...`.
  A caller three frames up has to be able to tell which resource failed.
- **Sentinel errors** for anything a caller branches on — `profile.ErrInvalid`,
  `promql.ErrVariablePosition` — and `errors.Is` to check.
- **Doc comments on every exported symbol**, starting with its name. Say *why*
  where the what is obvious from the signature.
- **`//nolint` needs a reason.** `nolintlint` requires it; prefer fixing the
  code, and prefer a documented exclusion in `.golangci.yml` over a scattered
  directive.
- **`go` 1.26.7** in `go.mod`, matching `pkg/*`. Tool versions live in
  `tools/go.mod` so a developer and CI build the same binary.

## Comments

Explain the reason, not the mechanics. `// increment i` is noise; `// zero, so
no timestamp is written` is the thing a reader cannot recover from the code.

Where a decision looks wrong at first glance, say why it is not — the deprecated
dashboard builder, the hand-written CR structs, the `ruleGroup` field set and
then dropped. Those comments are what stop the next person "fixing" them.
