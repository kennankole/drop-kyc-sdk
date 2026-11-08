# drop-kyc-sdk

Vendor-neutral Go SDKs for identity and business verification (KYC) behind
Drop's merchant and rider onboarding. A monorepo of independent modules, one
per capability: `verification/` today.

Application code depends on each module's interfaces and types, never on a
provider package directly, so a KYC provider can be added, reordered or
replaced (KRA, Smile ID, Youverify, Didit, …) without touching business
logic.

## Modules

### [`verification/`](verification)

A `Verifier` port that every provider implements, the vendor-neutral
`Subject` / `CheckType` / `Result` vocabulary, and an `Engine` that runs
checks against providers in priority order and falls back to the next one
when a provider is unavailable. Providers live in subpackages:

| Package | Provider | Checks |
|---|---|---|
| `verification/kra` | KRA GavaConnect (free): PIN Checker By ID, PIN Checker by PIN | `national_id` (ID + name of a person), `tax_pin` (PIN + name of a person or business) |

```go
import (
    "github.com/kennankole/drop-kyc-sdk/verification"
    "github.com/kennankole/drop-kyc-sdk/verification/kra"
)

client, _ := kra.NewClient(kra.SandboxBaseURL, key, secret, nil)
engine := verification.NewEngine(
    []verification.Verifier{kra.NewVerifier(client) /* , smileid.New(...) */},
    verification.WithObserver(myObserver),
)

results := engine.Run(ctx, &verification.Subject{
    Kind: verification.KindIndividual, Country: "KE",
    FullName: "Mary Wanjiru Njeri", NationalID: "12345678", TaxPIN: "A123456789Z",
}, verification.CheckNationalID, verification.CheckTaxPIN)
// One Result per check some provider supports: pass, fail, inconclusive,
// or unavailable (never read as a pass).
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the module and provider
conventions.
