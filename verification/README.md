# verification

Vendor-neutral KYC checks. Depend on this package; construct providers
(`verification/kra`, …) only at your composition root.

- `Subject`: who is checked (person or business, ID, names, tax PIN,
  documents as time-limited URLs).
- `CheckType`: `national_id`, `selfie_match`, `tax_pin`, `business_registry`,
  `driving_license`, `sim_swap`, `mobile_money_name`.
- `Result`: `pass`, `fail`, `inconclusive` or `unavailable`, with a reason
  and provider details.
- `Verifier`: the port a provider implements.
- `Engine`: runs the requested checks against providers in priority order;
  skips checks no provider supports; falls back on `ErrUnavailable`; reports
  every call to an `Observer`.
