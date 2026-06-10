# Agent instructions for `spanenc`

Go library (**MIT**): convert plain Go values to
`spanner.GenericColumnValue` (GCV) and derive column names / Spanner types
from Go structs, **mirroring the Cloud Spanner Go client library's internal
encoding semantics**. Built on [`spanvalue/gcvctor`](https://github.com/apstndb/spanvalue),
[`spantype/typector`](https://github.com/apstndb/spantype), and
[`structfields`](https://github.com/apstndb/structfields) (the Apache-2.0
exported fork of `cloud.google.com/go/internal/fields`; all upstream-derived
code lives THERE, never here — spanenc mirrors behavior with independent
code only). Alias **`sppb`** = `cloud.google.com/go/spanner/apiv1/spannerpb`.

## Commands

Tasks and tool versions live in **`mise.toml`** (`go`, `golangci-lint`).
Prefer **`mise run check`** (fmt-check, vet, build, test, lint); also
`mise run test-race`, `mise run fmt`. The `Makefile` is a thin wrapper
delegating to mise. CI (`.github/workflows/go.yml`) runs the same tasks via
`jdx/mise-action`; keep action versions current (mise-action v4+, checkout v6+).

## Upstream mirror policy (core invariant)

This package is a **behavioral derivative of `cloud.google.com/go/spanner`**;
the mirrored behavior currently tracks **v1.91.0**. Sources of truth in the
module cache:

| spanenc | upstream (value.go / mutation.go) |
|---------|-----------------------------------|
| `ValueOf` (encode.go) | `encodeValue` type-switch cases, in the same order |
| `convertCustomValue` / `customBaseGoType` (typeof.go) | `getDecodableSpannerType` + `convertCustomTypeValue` (encode half) |
| `encodeStructValue` | `encodeStruct` (declaration order, embedded rejected, tag via `Lookup` so `spanner:""` = unnamed field) |
| `structFields` / `fieldCache` (struct.go) | `fieldCache` + `spannerTagParser` (tag via `Get`; `;`-separated options, `->`/`readonly` = read-only since v1.86.0) |
| `validateNumeric` | `validateNumeric` (same algorithm, independent expression; default NumericError handling) |
| `github.com/apstndb/structfields` (dependency) | **exported fork of `cloud.google.com/go/internal/fields`** — upstream-derived code lives in that ASL2 module, keeping spanenc MIT |

When re-auditing against a newer spanner release: diff these functions
against upstream, update the tracked version in `doc.go` and `README.md`, and
extend `exactGoTypes` (typeof.go) together with the `ValueOf` switch — the
test suite cross-checks `TypeFromGoType` against `ValueOf` results.

**Mirrored-semantics version ≠ go.mod requirement.** `go.mod` declares only
the minimum spanner version whose APIs the code uses (currently the v1.84.1
floor inherited from spanvalue) so downstreams control the client version
under MVS; do NOT bump it just because the audited semantics version moved.
The `latest-deps` CI job tests against spanner@latest to catch drift.

Mirrored quirks are deliberate (do not "fix"): `==` sentinel comparison for
`spanner.CommitTimestamp`; nil named UUID-array slices converting to an empty
`[]uuid.UUID`; two different struct field listings (row-shaped = flattened
embedded; STRUCT values = embedded rejected); encodeStruct reads the raw tag,
so tag options leak verbatim into STRUCT field names (`"Name;readonly"`);
read-only fields stay in read-shaped listings (StructColumns / RowTypeFor /
StructColumnsAndValues) and are excluded only from Mutation* helpers, like
structToMutationParams; dead `Ptr` branch parity in `customBaseGoType`.

## Deliberate divergences (documented in doc.go; keep them)

Strictness so malformed GCVs never enter the spanvalue stack: untyped nil →
`ErrUntypedNil`; nil struct pointer in row-shaped helpers →
`ErrNilStructPointer`; GCV input with nil Type rejected; NUMERIC
loss-of-precision is per-call (`WithLossOfPrecisionHandling`, default
NumericError) and NEVER reads the client's package-global
`spanner.LossOfPrecisionHandling` (whose default is NumericRound); non-finite
floats and JSON use gcvctor canonical wire forms (strings / unescaped JSON)
instead of the client's NumberValue / HTML-escaped JSON.

## API map

- `ValueOf` — Go value → GCV (encodeValue mirror).
- `TypeFor[T]` / `TypeFromGoType` — static type inference; `ErrTypeNotInferable`
  for Encoder/GCV/NullProto*/interface types (value-dependent).
- `StructColumns[T]` / `StructColumnsFromGoType` — column names
  ([googleapis/google-cloud-go#13800](https://github.com/googleapis/google-cloud-go/issues/13800)).
- `RowTypeFor[T]` / `RowTypeFromGoType` — `*sppb.StructType` for writer metadata.
- `StructColumnsAndValues` — struct → columns + GCVs (spanvalue/writer
  `WriteValues`).
- `MutationColumnsAndValues` / `MutationMap` — struct → plain Go cols/vals or
  map for `spanner.Insert/Update/Replace(...)` / `*Map` constructors; column
  masks via `WithColumns` (include) / `WithoutColumns` (exclude), strict
  (`ErrInvalidColumnMask` on unknown/read-only-in-include/combined). Values
  are NOT GCV-encoded (the client encodes them).
- `ValuesFromSlice[T]` / `ArrayValueFromSlice[T]` — homogeneous slices;
  interface element types rejected via static inference; nil slice = typed
  NULL ARRAY at the GCV level.

## Tests

`t.Parallel()`, `cmp.Diff` + `protocmp.Transform()`. Expected GCVs built from
`typector` + `structpb`, not from the helpers under test. Keep: the
ValueOf↔TypeFromGoType consistency check inside `TestValueOf`; decode
round-trips through the real client's `GenericColumnValue.Decode`;
`internal/fields` upstream tests (ported, `tEqual` replaces testutil).

## Dependencies & releases

- All dependencies are tagged releases (spanvalue v0.7.1+ is required for
  gcvctor's UTC-timestamp wire format; `structfields` and its nested
  `structfields/spannertag` module version independently).
- Per-version truth: GitHub Releases (no in-repo CHANGELOG). Experimental
  until encodeValue parity is proven; English only on github.com. Published
  versions are immutable — never re-tag (proxy.golang.org caches
  aggressively).
