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
// v1.84.1.
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
//   - [RowTypeFor] / [RowTypeFromGoType]: row-shaped
//     [cloud.google.com/go/spanner/apiv1/spannerpb.StructType] for writer
//     metadata.
//   - [StructColumnsAndValues]: one struct → column names + GCVs, for
//     GCV-level consumers such as [github.com/apstndb/spanvalue/writer].
//   - [MutationColumnsAndValues] / [MutationMap]: one struct → cols/vals or
//     map with plain Go values, for the non-Struct mutation constructors
//     ([cloud.google.com/go/spanner.Update],
//     [cloud.google.com/go/spanner.UpdateMap], ...), enabling column masking
//     by name.
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
//     declaration order.
//   - STRUCT-typed values ([ValueOf] on a struct, [TypeFor]) use the
//     encodeStruct listing: declaration order, embedded fields rejected with
//     [ErrEmbeddedStructField], and `spanner:""` producing an unnamed field.
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
//   - NUMERIC precision is always validated (the client validates only under
//     its default NumericError loss-of-precision handling, which is also the
//     behavior mirrored here).
//   - Non-finite FLOAT64/FLOAT32 values and JSON payloads use the canonical
//     wire forms produced by [github.com/apstndb/spanvalue/gcvctor]
//     ("NaN"/"Infinity" strings; compact JSON without HTML escaping); the
//     client sends a raw protobuf NumberValue and HTML-escaped JSON. Both
//     forms are semantically equivalent and accepted by Spanner.
//
// The package is experimental: the API may change while encodeValue parity
// is being proven against client library releases.
package spanenc
