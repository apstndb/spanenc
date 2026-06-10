// Package spanenc converts plain Go values into
// [cloud.google.com/go/spanner.GenericColumnValue] (GCV) values and derives
// column names and Spanner types from Go structs, following the Cloud
// Spanner Go client library's own encoding semantics — the `spanner` struct
// tag rules and the Go type coverage of statement parameters and mutations.
//
// The client library keeps its encoding internal (encodeValue,
// structToMutationParams, and the internal fields cache); this package
// mirrors those semantics on top of [github.com/apstndb/spanvalue/gcvctor]
// constructors so the results compose with the spanvalue formatting and
// writer stack. The mirrored behavior tracks cloud.google.com/go/spanner
// v1.91.0.
//
// # API overview
//
//   - [ValueOf]: Go value → GCV, mirroring encodeValue (typed NULLs from nil
//     pointers and nil slices, spanner.Null* wrappers, spanner.Encoder,
//     protobuf messages and enums, named variants of base types, Go structs
//     as STRUCT values, spanner.CommitTimestamp).
//   - [TypeFor] / [TypeFromGoType]: Go type →
//     [cloud.google.com/go/spanner/apiv1/spannerpb.Type], the type half of
//     [ValueOf] without a value.
//   - [StructColumns] / [StructColumnsFromGoType]: `spanner`-tagged column
//     names from a struct type, as requested in
//     https://github.com/googleapis/google-cloud-go/issues/13800.
//   - [RowTypeFor] / [RowTypeFromGoType] and [ResultSetMetadataFor] /
//     [ResultSetMetadataFromGoType]: row-shaped
//     [cloud.google.com/go/spanner/apiv1/spannerpb.StructType] /
//     [cloud.google.com/go/spanner/apiv1/spannerpb.ResultSetMetadata] for
//     writer metadata and client-side virtual result sets.
//   - [StructColumnsAndValues]: one struct → column names + GCVs, for
//     GCV-level consumers such as [github.com/apstndb/spanvalue/writer].
//   - [MutationColumnsAndValues] / [MutationMap]: one struct → cols/vals or
//     map with plain Go values, for the non-Struct mutation constructors
//     ([cloud.google.com/go/spanner.Update],
//     [cloud.google.com/go/spanner.UpdateMap], ...). An update-mask-style
//     column mask can be written as an include list ([WithColumns]) or an
//     exclude list ([WithoutColumns]).
//   - [ParamsMap]: one struct → map with plain Go values for
//     [cloud.google.com/go/spanner.Statement] Params; read-only fields are
//     included (they are ordinary bindable values), and the same column
//     masks apply.
//   - [ValuesFromSlice] / [ArrayValueFromSlice]: homogeneous slices →
//     (element type, wire values) or an ARRAY GCV; heterogeneous-capable
//     (interface) element types are rejected.
//
// # Struct field listings
//
// Following the client, there are two different struct field listings:
//
//   - Row-shaped helpers ([StructColumns], [RowTypeFor],
//     [StructColumnsAndValues], [MutationColumnsAndValues], [MutationMap])
//     use the mutation/ToStruct listing: exported fields, embedded struct
//     fields flattened with Go's shadowing rules, `spanner:"-"` skipped,
//     declaration order. Tags split on ";" with the column name first;
//     `spanner:"->"` or a `readonly` part marks the field read-only (since
//     spanner v1.86.0). Read-only fields stay in the read-shaped listings
//     ([StructColumns], [RowTypeFor], [StructColumnsAndValues]) and are
//     excluded from the write-shaped ones ([MutationColumnsAndValues],
//     [MutationMap]), mirroring structToMutationParams.
//   - STRUCT-typed values ([ValueOf] on a struct, [TypeFor]) use the
//     encodeStruct listing: declaration order, embedded fields rejected with
//     [ErrEmbeddedStructField], and `spanner:""` producing an unnamed field.
//     encodeStruct reads the raw tag, so tag options leak into STRUCT field
//     names verbatim (`spanner:"Name;readonly"` yields a field literally
//     named "Name;readonly"); this mirrors the client.
//
// # Divergences from the client library
//
// This package is strict where the client is lenient, so malformed GCVs
// never enter the spanvalue stack:
//
//   - Untyped nil returns [ErrUntypedNil]; the client sends a NULL without
//     type information.
//   - A nil pointer to struct passed to [MutationColumnsAndValues],
//     [MutationMap], or [StructColumnsAndValues] returns
//     [ErrNilStructPointer]; the client silently builds an empty mutation.
//   - [cloud.google.com/go/spanner.GenericColumnValue] inputs with a nil
//     Type are rejected.
//   - NUMERIC loss-of-precision handling is an explicit per-call option
//     ([WithLossOfPrecisionHandling], reusing the client's
//     [cloud.google.com/go/spanner.LossOfPrecisionHandlingOption]
//     vocabulary); the package-global
//     [cloud.google.com/go/spanner.LossOfPrecisionHandling] is never read.
//     The default is [cloud.google.com/go/spanner.NumericError] (validate),
//     while the client's global defaults to NumericRound (silent rounding).
//   - Non-finite FLOAT64/FLOAT32 values and JSON payloads use the canonical
//     wire forms produced by [github.com/apstndb/spanvalue/gcvctor]
//     ("NaN"/"Infinity" strings; compact JSON without HTML escaping); the
//     client sends a raw protobuf NumberValue and HTML-escaped JSON. Both
//     forms are semantically equivalent and accepted by Spanner.
//
// The package is experimental: the API may change while encodeValue parity
// is being proven against client library releases.
package spanenc
