package spanenc

import (
	"iter"
	"reflect"

	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/apstndb/spantype/typector"
	"github.com/apstndb/spanvalue/gcvctor"
	"google.golang.org/protobuf/proto"

	fields "github.com/apstndb/structfields"
)

// RowEncoder is a compiled row codec for a struct type T: the field listing,
// column mask, and row type are resolved once at construction, so encoding
// many rows avoids re-deriving them per row (and re-validating the mask) the
// way repeated [StructColumnsAndValues] calls would.
//
// It uses the read-shaped (ToStruct) field listing like
// [StructColumnsAndValues]: read-only fields are included, and an include
// mask may name them. Construct with [NewRowEncoder].
type RowEncoder[T any] struct {
	fields     fields.List
	columns    []string
	rowType    *sppb.StructType
	rowTypeErr error
}

// NewRowEncoder compiles a [RowEncoder] for T, which must be a struct or
// pointer-to-struct type ([ErrNotStruct] otherwise). The optional column
// mask is validated here once ([ErrInvalidColumnMask] like [ParamsMap]) and
// applied to every output, keeping struct declaration order.
func NewRowEncoder[T any](opts ...ColumnMaskOption) (*RowEncoder[T], error) {
	cfg := newColumnMaskConfig(opts)
	fl, err := structFields(reflect.TypeFor[T]())
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(fl, false); err != nil {
		return nil, err
	}
	kept := make(fields.List, 0, len(fl))
	columns := make([]string, 0, len(fl))
	for _, f := range fl {
		if !cfg.keep(f.Name) {
			continue
		}
		kept = append(kept, f)
		columns = append(columns, f.Name)
	}
	enc := &RowEncoder[T]{fields: kept, columns: columns}
	enc.rowType, enc.rowTypeErr = rowTypeOfFields(kept)
	return enc, nil
}

// MustNewRowEncoder is [NewRowEncoder] panicking on error, for package-level
// encoders of compile-time-known struct types where a failure is a
// programming error. Prefer it over local panic-on-error wrappers, like the
// Must* constructors in [github.com/apstndb/spanvalue/gcvctor].
func MustNewRowEncoder[T any](opts ...ColumnMaskOption) *RowEncoder[T] {
	enc, err := NewRowEncoder[T](opts...)
	if err != nil {
		panic(err)
	}
	return enc
}

// rowTypeOfFields derives the masked row type from an already-listed field
// set, like [RowTypeFromGoType] does for the full listing.
func rowTypeOfFields(fl fields.List) (*sppb.StructType, error) {
	stf := make([]*sppb.StructType_Field, len(fl))
	for i, f := range fl {
		ft, err := TypeFromGoType(f.Type)
		if err != nil {
			return nil, &gcvctor.StructFieldError{Index: i, Name: f.Name, Err: err}
		}
		stf[i] = typector.NameTypeToStructTypeField(f.Name, ft)
	}
	return typector.StructTypeFieldsToStructType(stf).GetStructType(), nil
}

// Columns returns the masked column names in struct declaration order.
// The returned slice is a copy.
//
// Prefer Columns when consumers only need names (string-only headers);
// use [RowEncoder.ResultSetMetadata] only when they need Spanner types —
// switching a consumer from names to metadata typically changes how it
// renders headers.
func (e *RowEncoder[T]) Columns() []string {
	out := make([]string, len(e.columns))
	copy(out, e.columns)
	return out
}

// RowType returns the masked row type with statically inferred field types
// (see [TypeFromGoType]); fields whose Spanner type is not inferable from
// the Go type make RowType fail while [RowEncoder.Values] may still succeed.
// The returned message is a fresh clone.
func (e *RowEncoder[T]) RowType() (*sppb.StructType, error) {
	if e.rowTypeErr != nil {
		return nil, e.rowTypeErr
	}
	return proto.Clone(e.rowType).(*sppb.StructType), nil
}

// ResultSetMetadata wraps [RowEncoder.RowType] into a
// [sppb.ResultSetMetadata] for result-set consumers such as
// [github.com/apstndb/spanvalue/writer]'s WithMetadata.
func (e *RowEncoder[T]) ResultSetMetadata() (*sppb.ResultSetMetadata, error) {
	rowType, err := e.RowType()
	if err != nil {
		return nil, err
	}
	return &sppb.ResultSetMetadata{RowType: rowType}, nil
}

// MustResultSetMetadata is [RowEncoder.ResultSetMetadata] panicking on error.
// Whether the row type resolves is deterministic for a given T and column
// mask (it is computed once at construction; see [RowEncoder.RowType]), so
// for package-level encoders of compile-time-known struct types — the
// [MustNewRowEncoder] pattern — a failure is a programming error. Note that
// [MustNewRowEncoder] alone does not guarantee an inferable row type;
// hoisting MustResultSetMetadata next to it surfaces that failure at
// initialization instead of per call. Each call returns a fresh clone.
func (e *RowEncoder[T]) MustResultSetMetadata() *sppb.ResultSetMetadata {
	md, err := e.ResultSetMetadata()
	if err != nil {
		panic(err)
	}
	return md
}

// Values encodes one row: the masked fields of v as
// [spanner.GenericColumnValue] slices aligned with [RowEncoder.Columns].
// A nil pointer v returns [ErrNilStructPointer]. Options configure the
// per-field encoding; see [WithLossOfPrecisionHandling].
//
// For display cells, pass the result to
// [github.com/apstndb/spanvalue.FormatRowColumns]; for file export, pass it
// to a [github.com/apstndb/spanvalue/writer] writer.
func (e *RowEncoder[T]) Values(v T, opts ...EncodeOption) ([]spanner.GenericColumnValue, error) {
	cfg := newEncodeConfig(opts)
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, ErrNilStructPointer
		}
		rv = rv.Elem()
	}
	values := make([]spanner.GenericColumnValue, len(e.fields))
	for i, f := range e.fields {
		fgcv, err := encodeValue(cfg, rv.FieldByIndex(f.Index).Interface())
		if err != nil {
			return nil, &gcvctor.StructFieldError{Index: i, Name: f.Name, Err: err}
		}
		values[i] = fgcv
	}
	return values, nil
}

// Row encodes one row as a [cloud.google.com/go/spanner.Row] whose columns
// align with [RowEncoder.Columns]. It is [RowEncoder.Values] followed by
// [cloud.google.com/go/spanner.NewRow]; NewRow routes
// [spanner.GenericColumnValue] inputs through the client's encodeValue,
// which deep-clones Type and Value without re-encoding, so the row carries
// exactly the values this encoder produced (including typed NULLs) and does
// not alias encoder output.
//
// Use Row when a consumer takes *spanner.Row — for example
// [github.com/apstndb/spanvalue.FormatConfig.FormatRow] display pipelines or
// [github.com/apstndb/spanvalue/writer] RowIteratorWriter sinks — so
// client-side (virtual) result sets flow through the same code paths as
// server query results.
func (e *RowEncoder[T]) Row(v T, opts ...EncodeOption) (*spanner.Row, error) {
	gcvs, err := e.Values(v, opts...)
	if err != nil {
		return nil, err
	}
	vals := make([]any, len(gcvs))
	for i := range gcvs {
		vals[i] = gcvs[i]
	}
	return spanner.NewRow(e.columns, vals)
}

// Rows returns an iterator over items encoded with [RowEncoder.Row].
// Encoding is lazy: each row is encoded only when yielded, so callers can
// stop early without paying for the remaining items. When an item fails to
// encode, the iterator yields (nil, err) once and stops.
//
// The (row, error) pairing matches fallible row sources, so a row-based
// sink can range over it and abort on the first error.
func (e *RowEncoder[T]) Rows(items []T, opts ...EncodeOption) iter.Seq2[*spanner.Row, error] {
	return func(yield func(*spanner.Row, error) bool) {
		for _, item := range items {
			row, err := e.Row(item, opts...)
			if !yield(row, err) || err != nil {
				return
			}
		}
	}
}
