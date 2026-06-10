# spanenc

[![Go Reference](https://pkg.go.dev/badge/github.com/apstndb/spanenc.svg)](https://pkg.go.dev/github.com/apstndb/spanenc)

Convert plain Go values into Cloud Spanner `GenericColumnValue` (GCV) values
and derive column names and Spanner types from Go structs, following the
[Cloud Spanner Go client library](https://pkg.go.dev/cloud.google.com/go/spanner)'s
own encoding semantics: the `spanner` struct tag rules and the Go type
coverage of statement parameters and mutations.

The client library keeps its encoding internal (`encodeValue`,
`structToMutationParams`, and the internal `fields` cache). This package
mirrors those semantics on top of
[`github.com/apstndb/spanvalue/gcvctor`](https://pkg.go.dev/github.com/apstndb/spanvalue/gcvctor)
constructors, so the results compose with the
[spanvalue](https://github.com/apstndb/spanvalue) formatting and writer stack.

**Status: experimental.** The API may change while `encodeValue` parity is
proven against client library releases. The mirrored behavior currently
tracks `cloud.google.com/go/spanner` **v1.91.0**.

## Motivation

- [googleapis/google-cloud-go#13800](https://github.com/googleapis/google-cloud-go/issues/13800)
  asks for a `StructColumns()` helper that derives `Read` columns from the
  same struct used with `Row.ToStruct`. [`StructColumns`](https://pkg.go.dev/github.com/apstndb/spanenc#StructColumns)
  provides it with the exact field listing the client uses.
- The client offers no public Go value → `*structpb.Value` / `*sppb.Type`
  encoding, but GCV-level tooling (exporters, REPLs, test fixtures, proto-level
  API callers) needs one that behaves identically to the client.
- Mutation constructors come in three flavors (`Update`, `UpdateMap`,
  `UpdateStruct`); only the struct flavor understands tags, and it cannot mask
  columns. [`MutationColumnsAndValues`](https://pkg.go.dev/github.com/apstndb/spanenc#MutationColumnsAndValues) /
  [`MutationMap`](https://pkg.go.dev/github.com/apstndb/spanenc#MutationMap)
  extract tag-derived cols/vals (plain Go values, encoded later by the client
  itself) so the other two flavors get tag support and per-column masking.

## API overview

| Function | Input | Output |
|----------|-------|--------|
| `ValueOf` | Go value | `spanner.GenericColumnValue` |
| `TypeFor[T]` / `TypeFromGoType` | Go type | `*sppb.Type` |
| `StructColumns[T]` / `StructColumnsFromGoType` | struct type | `[]string` column names |
| `RowTypeFor[T]` / `RowTypeFromGoType` | struct type | `*sppb.StructType` row type |
| `ResultSetMetadataFor[T]` / `ResultSetMetadataFromGoType` | struct type | `*sppb.ResultSetMetadata` (for `writer.WithMetadata`, virtual result sets) |
| `StructColumnsAndValues` | struct value | `[]string`, `[]GCV` |
| `NewRowEncoder[T]` | struct type (+ mask) | `*RowEncoder[T]`: compiled `Columns` / `RowType` / `ResultSetMetadata` / per-row `Values` |
| `MutationColumnsAndValues` | struct value | `[]string`, `[]any` (for `spanner.Insert`/`Update`/`Replace`...) |
| `MutationMap` | struct value | `map[string]any` (for `spanner.InsertMap`/`UpdateMap`...) |
| `ParamsMap` | struct value | `map[string]any` (for `spanner.Statement` Params; read-only fields included) |
| `ValuesFromSlice[T]` | homogeneous slice | `*sppb.Type` (element), `[]*structpb.Value` |
| `ArrayValueFromSlice[T]` | homogeneous slice | ARRAY GCV (nil slice → typed NULL ARRAY) |

Slice helpers enforce homogeneity through the static element type: interface
element types (which could hold heterogeneous values) are rejected before any
element is examined.

Options:

- `WithColumns(...)` / `WithoutColumns(...)` — update-mask-style include /
  exclude column masks for `MutationColumnsAndValues` / `MutationMap` /
  `ParamsMap` (struct declaration order preserved; unknown columns,
  read-only columns in a write-shaped include list, or combining both kinds
  return `ErrInvalidColumnMask`).
- `WithLossOfPrecisionHandling(spanner.NumericRound)` — per-call NUMERIC
  loss-of-precision control for the encoding helpers, reusing the client's
  enum; the client's package-global `spanner.LossOfPrecisionHandling` is
  never read. Default: `spanner.NumericError` (validate), unlike the
  client's global default of NumericRound.

## Examples

Derive `Read` columns from a tagged struct (the issue #13800 use case):

```go
type Singer struct {
    SingerID  int64 `spanner:"SingerId"`
    FirstName string
    Internal  string `spanner:"-"`
}

columns, _ := spanenc.StructColumns[Singer]() // [SingerId FirstName]
iter := client.Single().Read(ctx, "Singers", spanner.AllKeys(), columns)
```

Mask mutation columns by name (include or exclude):

```go
cols, vals, _ := spanenc.MutationColumnsAndValues(singer,
    spanenc.WithoutColumns("CreatedAt"))
m := spanner.Update("Singers", cols, vals)
```

Stream Go structs through
[`github.com/apstndb/spanvalue/writer`](https://pkg.go.dev/github.com/apstndb/spanvalue/writer)
(CSV/TSV/JSONL/SQL INSERT exporters for GCV rows):

```go
names, values, _ := spanenc.StructColumnsAndValues(singer)
w, _ := writer.NewCSVWriter(os.Stdout, writer.WithColumnNames(names))
_ = w.WriteValues(names, values)
_ = w.Flush()
```

For many rows of one struct type (for example client-side virtual result
sets), compile a `RowEncoder` once:

```go
enc, _ := spanenc.NewRowEncoder[Singer]()
metadata, _ := enc.ResultSetMetadata()
w, _ := writer.NewCSVWriter(os.Stdout,
    writer.DelimitedGCVExportOptions(metadata, spanvalue.SimpleFormatConfig(), spanvalue.IndexedUnnamedFieldNamer)...)
for _, s := range singers {
    values, _ := enc.Values(s)
    _ = w.WriteGCVs(values)
}
_ = w.Flush()
```

## Semantics notes

Following the client, there are two different struct field listings:

- **Row-shaped** (`StructColumns`, `RowTypeFor`, `StructColumnsAndValues`,
  `MutationColumnsAndValues`, `MutationMap`): the mutation/`ToStruct` listing
  — exported fields, embedded struct fields flattened with Go's shadowing
  rules, `spanner:"-"` skipped, declaration order. Read-only fields
  (`spanner:"->"` / `spanner:"Name;readonly"`, since spanner v1.86.0) are
  included in the read-shaped helpers and excluded from `MutationColumnsAndValues`
  / `MutationMap`, matching the client's mutation constructors.
- **STRUCT-typed values** (`ValueOf` on a struct, `TypeFor`): the
  `encodeStruct` listing — declaration order, embedded fields rejected,
  `spanner:""` producing an unnamed STRUCT field.

Deliberate divergences from the client (strictness so malformed GCVs never
enter the spanvalue stack) are documented in the
[package documentation](https://pkg.go.dev/github.com/apstndb/spanenc):
untyped nil and nil struct pointers return errors, NUMERIC precision is
validated by default (per-call `WithLossOfPrecisionHandling` instead of the
client's package-global), and non-finite floats / JSON use gcvctor's
canonical wire forms.

## Tracking upstream

This package is, by design, a behavioral mirror of
[googleapis/google-cloud-go](https://github.com/googleapis/google-cloud-go)'s
`spanner` package:

- Struct-field listing reuses
  [`github.com/apstndb/structfields`](https://github.com/apstndb/structfields),
  an exported fork of `cloud.google.com/go/internal/fields` (Apache License
  2.0) maintained as a separate module so that this module contains no
  upstream-derived code.
- `ValueOf` / `TypeFromGoType` mirror the *behavior* of `encodeValue` and
  `getDecodableSpannerType` with independent code; new client-supported Go
  types and Spanner types must be added here when upstream adds them.

The mirrored-semantics version is independent of the `go.mod` requirement:
`go.mod` declares only the minimum `cloud.google.com/go/spanner` providing
the APIs this module compiles against (currently the v1.84.1 floor inherited
from spanvalue), so downstream modules keep control of the client version
under MVS. CI additionally tests against the latest spanner release to catch
drift early.

When re-auditing against a newer client release, update the tracked version
in the package documentation — and bump `go.mod` only if newly used APIs
require it.

## License

MIT. The Apache-2.0 fork of upstream code lives separately in
[structfields](https://github.com/apstndb/structfields).
