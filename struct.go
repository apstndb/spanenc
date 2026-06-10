package spanenc

import (
	"fmt"
	"reflect"

	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/apstndb/spantype/typector"
	"github.com/apstndb/spanvalue/gcvctor"

	fields "github.com/apstndb/structfields"
	"github.com/apstndb/structfields/spannertag"
)

// fieldCache lists struct fields with the client's exact `spanner` tag
// semantics; the parser and cache are the structfields ports of the
// client's unexported spannerTagParser and fieldCache.
var fieldCache = spannertag.Cache

// isReadOnlyField reports whether a listed field carries the read-only tag
// (`spanner:"->"` or `spanner:"Name;readonly"`, since spanner v1.86.0).
func isReadOnlyField(f fields.Field) bool {
	tag, ok := f.ParsedTag.(spannertag.Tag)
	return ok && tag.ReadOnly
}

// structFields lists the row-shaped fields of a struct type with the same
// rules the client uses for mutations and ToStruct: exported fields,
// embedded structs flattened with Go's shadowing rules, `spanner` tag names,
// declaration order. Read-only fields are included; write-shaped helpers
// filter them out, mirroring structToMutationParams.
func structFields(t reflect.Type) (fields.List, error) {
	if t == nil {
		return nil, ErrNotStruct
	}
	if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: %v", ErrNotStruct, t)
	}
	return fieldCache.Fields(t)
}

// StructColumns returns the column names derived from T's fields and
// `spanner` tags, in declaration order, with the same field listing the
// client library uses for [cloud.google.com/go/spanner.Row.ToStruct]:
// exported fields only, embedded struct fields flattened, `spanner:"-"`
// skipped. Read-only fields (`spanner:"->"` or `spanner:"Name;readonly"`)
// are included, because they are readable; the write-shaped
// [MutationColumnsAndValues] and [MutationMap] exclude them like the
// client's mutation constructors do.
//
// It is the spanvalue-side answer to the StructColumns helper requested in
// https://github.com/googleapis/google-cloud-go/issues/13800, typically used
// to build the columns argument of Read calls from the same struct passed to
// ToStruct. T may be a struct or pointer-to-struct type.
func StructColumns[T any]() ([]string, error) {
	return StructColumnsFromGoType(reflect.TypeFor[T]())
}

// StructColumnsFromGoType is [StructColumns] for a [reflect.Type].
func StructColumnsFromGoType(t reflect.Type) ([]string, error) {
	fl, err := structFields(t)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(fl))
	for i, f := range fl {
		names[i] = f.Name
	}
	return names, nil
}

// RowTypeFor returns the [sppb.StructType] describing a row of T, pairing
// [StructColumns] names (read-only fields included) with statically inferred
// field types (see [TypeFromGoType]). It suits writer metadata such as
// [github.com/apstndb/spanvalue/writer]'s WithRowType, or the row_type of a
// ResultSetMetadata.
//
// Note that this row-shaped view flattens embedded struct fields, while a
// STRUCT-typed value of T ([TypeFor], [ValueOf]) rejects them; the client
// library has the same split between mutations/ToStruct and STRUCT
// parameters.
func RowTypeFor[T any]() (*sppb.StructType, error) {
	return RowTypeFromGoType(reflect.TypeFor[T]())
}

// ResultSetMetadataFor wraps [RowTypeFor] into a
// [sppb.ResultSetMetadata], the shape expected by result-set consumers such
// as [github.com/apstndb/spanvalue/writer]'s WithMetadata. It suits
// client-side virtual result sets (status rows, SHOW-style outputs) whose
// rows are Go structs rather than server responses.
func ResultSetMetadataFor[T any]() (*sppb.ResultSetMetadata, error) {
	return ResultSetMetadataFromGoType(reflect.TypeFor[T]())
}

// ResultSetMetadataFromGoType is [ResultSetMetadataFor] for a [reflect.Type].
func ResultSetMetadataFromGoType(t reflect.Type) (*sppb.ResultSetMetadata, error) {
	rowType, err := RowTypeFromGoType(t)
	if err != nil {
		return nil, err
	}
	return &sppb.ResultSetMetadata{RowType: rowType}, nil
}

// RowTypeFromGoType is [RowTypeFor] for a [reflect.Type].
func RowTypeFromGoType(t reflect.Type) (*sppb.StructType, error) {
	fl, err := structFields(t)
	if err != nil {
		return nil, err
	}
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

// structValue prepares the addressable struct value behind v for row-shaped
// helpers, mirroring structToMutationParams except that a nil pointer to
// struct returns [ErrNilStructPointer] instead of silently yielding an empty
// row.
func structValue(v any) (reflect.Value, fields.List, error) {
	if v == nil {
		return reflect.Value{}, nil, ErrNotStruct
	}
	rv := reflect.ValueOf(v)
	t := rv.Type()
	if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct {
		if rv.IsNil() {
			return reflect.Value{}, nil, fmt.Errorf("%w: %T", ErrNilStructPointer, v)
		}
		rv = rv.Elem()
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return reflect.Value{}, nil, fmt.Errorf("%w: %T", ErrNotStruct, v)
	}
	fl, err := fieldCache.Fields(t)
	if err != nil {
		return reflect.Value{}, nil, err
	}
	return rv, fl, nil
}

// StructColumnsAndValues converts a struct (or non-nil pointer to struct)
// into parallel column-name and [spanner.GenericColumnValue] slices using
// the ToStruct-shaped field listing (see [StructColumns]; read-only fields
// included) and [ValueOf] for each field. The result feeds GCV-level
// consumers such as [github.com/apstndb/spanvalue/writer]'s WriteValues or
// [github.com/apstndb/spanvalue.FormatRowColumns] for display cells, and
// stays aligned with [RowTypeFor].
//
// For the client library's own mutation constructors, use
// [MutationColumnsAndValues] or [MutationMap] instead, which keep plain Go
// values, exclude read-only fields, and let the client encode the values.
//
// Options configure the per-field encoding; see [WithLossOfPrecisionHandling].
func StructColumnsAndValues(v any, opts ...EncodeOption) ([]string, []spanner.GenericColumnValue, error) {
	cfg := newEncodeConfig(opts)
	rv, fl, err := structValue(v)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, len(fl))
	values := make([]spanner.GenericColumnValue, len(fl))
	for i, f := range fl {
		names[i] = f.Name
		fgcv, err := encodeValue(cfg, rv.FieldByIndex(f.Index).Interface())
		if err != nil {
			return nil, nil, &gcvctor.StructFieldError{Index: i, Name: f.Name, Err: err}
		}
		values[i] = fgcv
	}
	return names, values, nil
}

// MutationColumnsAndValues extracts column names and plain Go field values
// from a struct (or non-nil pointer to struct), mirroring the client's
// structToMutationParams: read-only fields (`spanner:"->"` or
// `spanner:"Name;readonly"`, since spanner v1.86.0) are excluded from the
// results. The results fit the cols/vals form of mutation constructors such
// as [spanner.Insert], [spanner.Update], and [spanner.Replace], so callers
// can mask columns by name before building the mutation — something the
// *Struct constructors cannot do.
//
// Values are returned as-is (no GCV conversion); the client library encodes
// them when the mutation is applied, so this helper accepts whatever
// InsertStruct accepts. A nil pointer returns [ErrNilStructPointer] where
// the client would silently produce an empty mutation.
//
// An update-mask-style column mask can be written as either an include list
// ([WithColumns]) or an exclude list ([WithoutColumns]); masked output keeps
// the struct declaration order, and mask mistakes (unknown columns,
// read-only columns in an include list, or combining both kinds) return
// [ErrInvalidColumnMask].
func MutationColumnsAndValues(v any, opts ...ColumnMaskOption) ([]string, []any, error) {
	cfg := newColumnMaskConfig(opts)
	rv, fl, err := structValue(v)
	if err != nil {
		return nil, nil, err
	}
	if err := cfg.validate(fl, true); err != nil {
		return nil, nil, err
	}
	cols := make([]string, 0, len(fl))
	vals := make([]any, 0, len(fl))
	for _, f := range fl {
		if isReadOnlyField(f) || !cfg.keep(f.Name) {
			continue
		}
		cols = append(cols, f.Name)
		vals = append(vals, rv.FieldByIndex(f.Index).Interface())
	}
	return cols, vals, nil
}

// ParamsMap extracts a column-name-to-Go-value map from a struct (or
// non-nil pointer to struct) for binding as
// [cloud.google.com/go/spanner.Statement] Params (parameter names are the
// `spanner`-tagged column names). Values are returned as-is (no GCV
// conversion); the client library encodes them when the statement runs.
//
// Unlike the write-shaped [MutationMap], read-only fields are included —
// they are ordinary bindable values in a statement. The [WithColumns] /
// [WithoutColumns] masks apply, and an include mask may name read-only
// columns.
//
// Duplicate column names (possible with explicit duplicate `spanner` tags)
// return an error rather than silently dropping a value.
func ParamsMap(v any, opts ...ColumnMaskOption) (map[string]any, error) {
	cfg := newColumnMaskConfig(opts)
	rv, fl, err := structValue(v)
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(fl, false); err != nil {
		return nil, err
	}
	m := make(map[string]any, len(fl))
	for _, f := range fl {
		if !cfg.keep(f.Name) {
			continue
		}
		if _, ok := m[f.Name]; ok {
			return nil, fmt.Errorf("spanenc: duplicate column name %q", f.Name)
		}
		m[f.Name] = rv.FieldByIndex(f.Index).Interface()
	}
	return m, nil
}

// MutationMap extracts a column-name-to-Go-value map from a struct (or
// non-nil pointer to struct) for the *Map mutation constructors
// ([spanner.InsertMap], [spanner.UpdateMap], [spanner.ReplaceMap],
// [spanner.InsertOrUpdateMap]). Read-only fields are excluded and the
// [WithColumns] / [WithoutColumns] masks apply, like
// [MutationColumnsAndValues].
//
// Duplicate column names (possible with explicit duplicate `spanner` tags)
// return an error rather than silently dropping a value.
func MutationMap(v any, opts ...ColumnMaskOption) (map[string]any, error) {
	cols, vals, err := MutationColumnsAndValues(v, opts...)
	if err != nil {
		return nil, err
	}
	m := make(map[string]any, len(cols))
	for i, c := range cols {
		if _, ok := m[c]; ok {
			return nil, fmt.Errorf("spanenc: duplicate column name %q", c)
		}
		m[c] = vals[i]
	}
	return m, nil
}
