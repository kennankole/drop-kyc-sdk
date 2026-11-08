# Contributing

This is a monorepo of independent Go modules, one per KYC capability
(`verification/` today). Each module has its own `go.mod`, is released with
its own tag prefix, and pulls in only the dependencies it needs.

## Adding a provider

1. Create a subpackage of the capability named after the provider
   (`verification/smileid`). The root package stays vendor-neutral; nothing
   outside the provider package names the provider.
2. Implement `verification.Verifier`: `Provider()` returns a short stable
   name, `Supports` lists the checks the provider answers, `Verify` maps the
   vendor's answer onto a `verification.Result`.
3. Map answers honestly:
   - an authoritative negative answer is `OutcomeFail`, returned with a nil
     error, so the Engine does not ask the next provider;
   - a near-miss or a check that does not apply is `OutcomeInconclusive`;
   - unreachable, timed out, bad credentials or a response you cannot parse
     is an error wrapping `verification.ErrUnavailable`, which makes the
     Engine fall back. Never turn an unrecognized response into a pass;
   - a subject missing data the check needs is `verification.ErrInvalidSubject`.
4. Put anything a reviewer needs (registered name, record number) in
   `Result.Details` under exported key constants.
5. Test against an `httptest` server; no test may call the real provider.

## Adding a capability

1. Create a top-level directory named after the capability, not a vendor.
2. `cd <dir> && go mod init github.com/kennankole/drop-kyc-sdk/<dir>`.
3. Vendor-neutral ports and types at the module root, stdlib only where
   practical; each provider in its own subpackage.
4. CI discovers every `go.mod`, nothing to edit.

## Conventions

- Every exported error a port can return is a sentinel in the module's root
  package, so callers `errors.Is` against a stable contract.
- Distinguish "the provider answered no" from "the provider could not be
  asked" (`ErrUnavailable`). Only the latter is fallback-worthy.
- Network calls are reported through the `Observer`, so applications wire
  metrics without the SDK depending on a metrics library.

## Releases

Every push to `main` runs `.github/scripts/release.sh`, which tags and
releases each module independently (`verification/vX.Y.Z`) from the
[Conventional Commits](https://www.conventionalcommits.org/) since its last
tag: `fix:`/`perf:` patch, `feat:` minor, `!` or `BREAKING CHANGE:` major;
other types release nothing on their own.
