package spanenc

import (
	"database/sql"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/apstndb/spantype/typector"
	"github.com/apstndb/spanvalue/gcvctor"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
)

// gcv shortens internal signatures; the exported API spells out
// spanner.GenericColumnValue.
type gcv = spanner.GenericColumnValue

// commitTimestampPlaceholderString is the sentinel string the client sends in
// place of spanner.CommitTimestamp; see commitTimestampPlaceholderString in
// cloud.google.com/go/spanner.
const commitTimestampPlaceholderString = "spanner.commit_timestamp()"

// ValueOf converts a Go value into a [spanner.GenericColumnValue], following
// the Cloud Spanner client library encoding semantics (encodeValue in
// cloud.google.com/go/spanner): the Go types accepted for statement
// parameters and mutation values, including the typed NULL rules (nil
// pointers and nil slices become typed NULLs), spanner.Null* wrappers,
// [spanner.Encoder] implementations, protobuf messages and enums, named
// variants of base types, Go structs as STRUCT values, and
// [spanner.CommitTimestamp].
//
// Deliberate divergences from the client library are listed in the package
// documentation; most notably an untyped nil returns [ErrUntypedNil] instead
// of a NULL without type information.
//
// Options configure encoding per call; see [WithLossOfPrecisionHandling]
// for the per-call counterpart of the client's package-global
// loss-of-precision handling.
func ValueOf(v any, opts ...EncodeOption) (spanner.GenericColumnValue, error) {
	return encodeValue(newEncodeConfig(opts), v)
}

// encodeValue is the option-threaded core of [ValueOf], mirroring the
// client's encodeValue type switch in the same case order.
func encodeValue(cfg encodeConfig, v any) (spanner.GenericColumnValue, error) {
	switch x := v.(type) {
	case nil:
		return gcv{}, ErrUntypedNil
	case spanner.Interval:
		return gcvctor.IntervalValue(x), nil
	case []spanner.Interval:
		return encodeSlice(x, typector.Interval(), pure(gcvctor.IntervalValue))
	case spanner.NullInterval:
		return gcvctor.IntervalFromNullable(x), nil
	case []spanner.NullInterval:
		return encodeSlice(x, typector.Interval(), pure(gcvctor.IntervalFromNullable))
	case string:
		return gcvctor.StringValue(x), nil
	case spanner.NullString:
		return gcvctor.StringFromNullable(x), nil
	case sql.NullString:
		return gcvctor.StringFromNullable(spanner.NullString{StringVal: x.String, Valid: x.Valid}), nil
	case []string:
		return encodeSlice(x, typector.String(), pure(gcvctor.StringValue))
	case []spanner.NullString:
		return encodeSlice(x, typector.String(), pure(gcvctor.StringFromNullable))
	case *string:
		return gcvctor.StringFromPtr(x), nil
	case []*string:
		return encodeSlice(x, typector.String(), pure(gcvctor.StringFromPtr))
	case []byte:
		return gcvctor.BytesFromSlice(x), nil
	case [][]byte:
		return encodeSlice(x, typector.Bytes(), pure(gcvctor.BytesFromSlice))
	case int:
		return gcvctor.Int64Value(int64(x)), nil
	case []int:
		return encodeSlice(x, typector.Int64(), func(e int) (gcv, error) { return gcvctor.Int64Value(int64(e)), nil })
	case int64:
		return gcvctor.Int64Value(x), nil
	case []int64:
		return encodeSlice(x, typector.Int64(), pure(gcvctor.Int64Value))
	case spanner.NullInt64:
		return gcvctor.Int64FromNullable(x), nil
	case []spanner.NullInt64:
		return encodeSlice(x, typector.Int64(), pure(gcvctor.Int64FromNullable))
	case *int64:
		return gcvctor.Int64FromPtr(x), nil
	case []*int64:
		return encodeSlice(x, typector.Int64(), pure(gcvctor.Int64FromPtr))
	case bool:
		return gcvctor.BoolValue(x), nil
	case []bool:
		return encodeSlice(x, typector.Bool(), pure(gcvctor.BoolValue))
	case spanner.NullBool:
		return gcvctor.BoolFromNullable(x), nil
	case []spanner.NullBool:
		return encodeSlice(x, typector.Bool(), pure(gcvctor.BoolFromNullable))
	case *bool:
		return gcvctor.BoolFromPtr(x), nil
	case []*bool:
		return encodeSlice(x, typector.Bool(), pure(gcvctor.BoolFromPtr))
	case float64:
		return gcvctor.Float64Value(x), nil
	case []float64:
		return encodeSlice(x, typector.Float64(), pure(gcvctor.Float64Value))
	case spanner.NullFloat64:
		return gcvctor.Float64FromNullable(x), nil
	case []spanner.NullFloat64:
		return encodeSlice(x, typector.Float64(), pure(gcvctor.Float64FromNullable))
	case *float64:
		return gcvctor.Float64FromPtr(x), nil
	case []*float64:
		return encodeSlice(x, typector.Float64(), pure(gcvctor.Float64FromPtr))
	case float32:
		return gcvctor.Float32Value(x), nil
	case []float32:
		return encodeSlice(x, typector.Float32(), pure(gcvctor.Float32Value))
	case spanner.NullFloat32:
		return gcvctor.Float32FromNullable(x), nil
	case []spanner.NullFloat32:
		return encodeSlice(x, typector.Float32(), pure(gcvctor.Float32FromNullable))
	case *float32:
		return gcvctor.Float32FromPtr(x), nil
	case []*float32:
		return encodeSlice(x, typector.Float32(), pure(gcvctor.Float32FromPtr))
	case big.Rat:
		return encodeNumeric(cfg, &x)
	case []big.Rat:
		return encodeSlice(x, typector.Numeric(), func(e big.Rat) (gcv, error) { return encodeNumeric(cfg, &e) })
	case spanner.NullNumeric:
		return encodeNullNumeric(cfg, x)
	case []spanner.NullNumeric:
		return encodeSlice(x, typector.Numeric(), func(e spanner.NullNumeric) (gcv, error) { return encodeNullNumeric(cfg, e) })
	case spanner.PGNumeric:
		// PGNumericFromNullable stores the payload string on the wire as-is,
		// without validation, matching the client.
		return gcvctor.PGNumericFromNullable(x), nil
	case []spanner.PGNumeric:
		return encodeSlice(x, typector.PGNumeric(), pure(gcvctor.PGNumericFromNullable))
	case spanner.NullJSON:
		// Since spanvalue v0.7.3, JSONFromNullable marshals Value like the
		// client (a Go string becomes a quoted JSON string on the wire).
		return gcvctor.JSONFromNullable(x)
	case []spanner.NullJSON:
		return encodeSlice(x, typector.JSON(), gcvctor.JSONFromNullable)
	case spanner.PGJsonB:
		return gcvctor.PGJSONBFromNullable(x)
	case []spanner.PGJsonB:
		return encodeSlice(x, typector.PGJSONB(), gcvctor.PGJSONBFromNullable)
	case *big.Rat:
		return encodeNumeric(cfg, x)
	case []*big.Rat:
		return encodeSlice(x, typector.Numeric(), func(e *big.Rat) (gcv, error) { return encodeNumeric(cfg, e) })
	case time.Time:
		return encodeTimestamp(x), nil
	case []time.Time:
		return encodeSlice(x, typector.Timestamp(), pure(encodeTimestamp))
	case spanner.NullTime:
		return encodeNullTime(x), nil
	case []spanner.NullTime:
		return encodeSlice(x, typector.Timestamp(), pure(encodeNullTime))
	case *time.Time:
		if x == nil {
			return gcvctor.NullFromCode(sppb.TypeCode_TIMESTAMP), nil
		}
		return encodeTimestamp(*x), nil
	case []*time.Time:
		return encodeSlice(x, typector.Timestamp(), func(e *time.Time) (gcv, error) {
			if e == nil {
				return gcvctor.NullFromCode(sppb.TypeCode_TIMESTAMP), nil
			}
			return encodeTimestamp(*e), nil
		})
	case civil.Date:
		return gcvctor.DateValue(x), nil
	case []civil.Date:
		return encodeSlice(x, typector.Date(), pure(gcvctor.DateValue))
	case spanner.NullDate:
		return gcvctor.DateFromNullable(x), nil
	case []spanner.NullDate:
		return encodeSlice(x, typector.Date(), pure(gcvctor.DateFromNullable))
	case *civil.Date:
		return gcvctor.DateFromPtr(x), nil
	case []*civil.Date:
		return encodeSlice(x, typector.Date(), pure(gcvctor.DateFromPtr))
	case uuid.UUID:
		return gcvctor.UUIDValue(x), nil
	case []uuid.UUID:
		return encodeSlice(x, typector.UUID(), pure(gcvctor.UUIDValue))
	case []*uuid.UUID:
		return encodeSlice(x, typector.UUID(), pure(gcvctor.UUIDFromPtr))
	case spanner.NullUUID:
		return gcvctor.UUIDFromNullable(x), nil
	case []spanner.NullUUID:
		return encodeSlice(x, typector.UUID(), pure(gcvctor.UUIDFromNullable))
	case uuid.NullUUID:
		return gcvctor.UUIDFromNullable(spanner.NullUUID{UUID: x.UUID, Valid: x.Valid}), nil
	case *uuid.UUID:
		return gcvctor.UUIDFromPtr(x), nil
	case *spanner.NullUUID:
		if x == nil {
			return gcvctor.NullFromCode(sppb.TypeCode_UUID), nil
		}
		return gcvctor.UUIDFromNullable(*x), nil
	case *uuid.NullUUID:
		if x == nil {
			return gcvctor.NullFromCode(sppb.TypeCode_UUID), nil
		}
		return gcvctor.UUIDFromNullable(spanner.NullUUID{UUID: x.UUID, Valid: x.Valid}), nil
	case spanner.GenericColumnValue:
		// The client deep-clones and passes the value through unchanged.
		// A nil Type is rejected here instead, to preserve the
		// spanvalue/gcvctor invariant that every GCV carries a usable Type.
		if x.Type == nil {
			return gcv{}, fmt.Errorf("%w: GenericColumnValue with nil Type", ErrUnsupportedType)
		}
		return gcv{
			Type:  proto.Clone(x.Type).(*sppb.Type),
			Value: proto.Clone(x.Value).(*structpb.Value),
		}, nil
	case []spanner.GenericColumnValue:
		// Mirrors the client, which rejects []GenericColumnValue.
		return gcv{}, fmt.Errorf("%w: %T", ErrUnsupportedType, v)
	case protoreflect.Enum:
		if enc, ok := v.(spanner.Encoder); ok {
			return encodeViaEncoder(cfg, enc)
		}
		return encodeProtoEnum(x)
	case spanner.NullProtoEnum:
		if x.Valid {
			return encodeValue(cfg, x.ProtoEnumVal)
		}
		return gcv{}, fmt.Errorf("%w: %T with Valid == false", ErrInvalidSource, v)
	case proto.Message:
		if enc, ok := v.(spanner.Encoder); ok {
			return encodeViaEncoder(cfg, enc)
		}
		return encodeProtoMessage(x)
	case spanner.NullProtoMessage:
		if x.Valid {
			return encodeValue(cfg, x.ProtoMessageVal)
		}
		return gcv{}, fmt.Errorf("%w: %T with Valid == false", ErrInvalidSource, v)
	default:
		if enc, ok := v.(spanner.Encoder); ok {
			return encodeViaEncoder(cfg, enc)
		}
		if converted, ok := convertCustomValue(v); ok {
			return encodeValue(cfg, converted)
		}
		t := reflect.TypeOf(v)
		switch {
		case t.Kind() == reflect.Struct,
			t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct:
			return encodeStructValue(cfg, v)
		case t.Kind() == reflect.Slice && (t.Elem().Implements(protoMsgReflectType) || t.Elem().Implements(protoEnumReflectType)):
			return encodeProtoArrayValue(cfg, v)
		case t.Kind() == reflect.Slice && isStructOrStructPtr(t.Elem()):
			return encodeStructArrayValue(cfg, v)
		}
		return gcv{}, fmt.Errorf("%w: %T", ErrUnsupportedType, v)
	}
}

// encodeFunc converts one element of a homogeneous family.
type encodeFunc[T any] func(T) (spanner.GenericColumnValue, error)

// pure adapts an infallible constructor to encodeFunc.
func pure[T any](f func(T) spanner.GenericColumnValue) encodeFunc[T] {
	return func(v T) (gcv, error) { return f(v), nil }
}

// encodeSlice mirrors the client's per-case slice handling in encodeValue: a
// nil Go slice is a typed NULL ARRAY, a non-nil slice encodes per element.
// Element failures are wrapped in [gcvctor.ArrayElementError].
func encodeSlice[T any](vs []T, elemType *sppb.Type, f encodeFunc[T]) (spanner.GenericColumnValue, error) {
	if vs == nil {
		return gcvctor.NullArrayOf(elemType), nil
	}
	elems := make([]gcv, len(vs))
	for i, v := range vs {
		e, err := f(v)
		if err != nil {
			return gcv{}, &gcvctor.ArrayElementError{Index: i, Err: err}
		}
		elems[i] = e
	}
	return gcvctor.ArrayValueOf(elemType, elems...)
}

// encodeNumeric validates precision and scale like the client under its
// default NumericError loss-of-precision handling, then defers to
// [gcvctor.NumericValue] (nil yields a typed NULL NUMERIC, matching *big.Rat
// handling in encodeValue).
func encodeNumeric(cfg encodeConfig, v *big.Rat) (spanner.GenericColumnValue, error) {
	if err := validateNumeric(cfg, v); err != nil {
		return gcv{}, err
	}
	return gcvctor.NumericValue(v), nil
}

// encodeNullNumeric validates like [encodeNumeric], then defers to
// [gcvctor.NumericFromNullable].
func encodeNullNumeric(cfg encodeConfig, v spanner.NullNumeric) (spanner.GenericColumnValue, error) {
	if v.Valid {
		if err := validateNumeric(cfg, &v.Numeric); err != nil {
			return gcv{}, err
		}
	}
	return gcvctor.NumericFromNullable(v), nil
}

// encodeTimestamp encodes a TIMESTAMP, honoring the
// [spanner.CommitTimestamp] sentinel like the client does for both scalar
// and array elements.
func encodeTimestamp(v time.Time) spanner.GenericColumnValue {
	// The == comparison is intentional and mirrors the client: the sentinel
	// is detected by exact representation (including its synthetic location),
	// not by instant equality, so an unrelated time.Time denoting the same
	// instant is NOT treated as a commit timestamp. time.Time.Equal would
	// break that.
	if v == spanner.CommitTimestamp { //nolint:staticcheck // QF1009: see above
		return gcvctor.StringBasedValueFromCode(sppb.TypeCode_TIMESTAMP, commitTimestampPlaceholderString)
	}
	return gcvctor.TimestampValue(v)
}

func encodeNullTime(v spanner.NullTime) spanner.GenericColumnValue {
	if !v.Valid {
		return gcvctor.NullFromCode(sppb.TypeCode_TIMESTAMP)
	}
	return encodeTimestamp(v.Time)
}

// encodeViaEncoder mirrors the client's spanner.Encoder handling: encode
// whatever EncodeSpanner returns.
func encodeViaEncoder(cfg encodeConfig, enc spanner.Encoder) (spanner.GenericColumnValue, error) {
	nv, err := enc.EncodeSpanner()
	if err != nil {
		return gcv{}, err
	}
	return encodeValue(cfg, nv)
}

// encodeProtoEnum mirrors the protoreflect.Enum case of encodeValue,
// including a typed nil enum pointer becoming a typed NULL ENUM.
func encodeProtoEnum(v protoreflect.Enum) (spanner.GenericColumnValue, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return gcvctor.NullOf(typector.FQNToEnumType(enumFQNFromGoType(rv.Type()))), nil
	}
	return gcvctor.EnumValue(string(v.Descriptor().FullName()), int64(v.Number())), nil
}

// encodeProtoMessage mirrors the proto.Message case of encodeValue: an
// invalid (typed nil) message becomes a typed NULL PROTO.
func encodeProtoMessage(v proto.Message) (spanner.GenericColumnValue, error) {
	fqn := string(v.ProtoReflect().Descriptor().FullName())
	if !v.ProtoReflect().IsValid() {
		return gcvctor.NullOf(typector.FQNToProtoType(fqn)), nil
	}
	b, err := proto.Marshal(v)
	if err != nil {
		return gcv{}, err
	}
	return gcvctor.ProtoValue(fqn, b), nil
}

// convertCustomValue mirrors getDecodableSpannerType plus
// convertCustomTypeValue: named variants of base types convert to the base
// type and re-encode. Slices convert per element.
func convertCustomValue(v any) (any, bool) {
	t := reflect.TypeOf(v)
	base, ok := customBaseGoType(t)
	if !ok {
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if t.Kind() == reflect.Slice {
		if rv.IsNil() {
			// Upstream quirk: a nil named slice of UUID-convertible arrays
			// converts to an EMPTY []uuid.UUID (encoded as an empty ARRAY),
			// while every other nil named slice stays nil (typed NULL ARRAY).
			if base == reflect.TypeFor[[]uuid.UUID]() {
				return []uuid.UUID{}, true
			}
			return reflect.Zero(base).Interface(), true
		}
		out := reflect.MakeSlice(base, rv.Len(), rv.Cap())
		for i := 0; i < rv.Len(); i++ {
			out.Index(i).Set(rv.Index(i).Convert(base.Elem()))
		}
		return out.Interface(), true
	}
	return rv.Convert(base).Interface(), true
}

func isStructOrStructPtr(t reflect.Type) bool {
	return t.Kind() == reflect.Struct || (t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct)
}

// encodeStructValue mirrors the client's encodeStruct: fields in declaration
// order, embedded fields rejected, unexported fields skipped, field names
// from the `spanner` tag via Lookup (so `spanner:""` yields an unnamed
// field), and a nil pointer to struct becoming a typed NULL STRUCT whose
// type is derived from the zero value.
func encodeStructValue(cfg encodeConfig, v any) (spanner.GenericColumnValue, error) {
	typ := reflect.TypeOf(v)
	val := reflect.ValueOf(v)
	if typ.Kind() == reflect.Pointer && typ.Elem().Kind() == reflect.Struct {
		typ = typ.Elem()
		if val.IsNil() {
			// Like the client, derive the STRUCT type by encoding the zero
			// value, so value-dependent fields (for example spanner.Encoder
			// implementations) type identically to non-NULL encodes.
			zero, err := encodeStructValue(cfg, reflect.Zero(typ).Interface())
			if err != nil {
				return gcv{}, err
			}
			return gcvctor.NullOf(zero.Type), nil
		}
		val = val.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return gcv{}, fmt.Errorf("%w: %T", ErrUnsupportedType, v)
	}
	names := make([]string, 0, typ.NumField())
	gcvs := make([]gcv, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		sf := typ.Field(i)
		fval := val.Field(i)
		if sf.Anonymous {
			return gcv{}, fmt.Errorf("%w: field %s of %v", ErrEmbeddedStructField, sf.Name, typ)
		}
		if !fval.CanInterface() { // unexported
			continue
		}
		fname, ok := sf.Tag.Lookup("spanner")
		if !ok {
			fname = sf.Name
		}
		fgcv, err := encodeValue(cfg, fval.Interface())
		if err != nil {
			return gcv{}, &gcvctor.StructFieldError{Index: i, Name: fname, Err: err}
		}
		names = append(names, fname)
		gcvs = append(gcvs, fgcv)
	}
	return gcvctor.StructValueOf(names, gcvs)
}

// encodeStructArrayValue mirrors the client's encodeStructArray: the element
// STRUCT type comes from the zero value, a nil slice is a typed NULL
// ARRAY<STRUCT>, and elements (including nil struct pointers) encode like
// encodeStructValue.
func encodeStructArrayValue(cfg encodeConfig, v any) (spanner.GenericColumnValue, error) {
	rv := reflect.ValueOf(v)
	etyp := rv.Type().Elem()
	if etyp.Kind() == reflect.Pointer {
		etyp = etyp.Elem()
	}
	zero, err := encodeStructValue(cfg, reflect.Zero(etyp).Interface())
	if err != nil {
		return gcv{}, err
	}
	elemType := zero.Type
	if rv.IsNil() {
		return gcvctor.NullArrayOf(elemType), nil
	}
	elems := make([]gcv, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		e, err := encodeStructValue(cfg, rv.Index(i).Interface())
		if err != nil {
			return gcv{}, &gcvctor.ArrayElementError{Index: i, Err: err}
		}
		elems[i] = e
	}
	return gcvctor.ArrayValueOf(elemType, elems...)
}

// encodeProtoArrayValue mirrors the client's encodeProtoArray for slices
// whose element type implements proto.Message or protoreflect.Enum.
func encodeProtoArrayValue(cfg encodeConfig, v any) (spanner.GenericColumnValue, error) {
	rv := reflect.ValueOf(v)
	et := rv.Type().Elem()
	var elemType *sppb.Type
	switch {
	case et.Implements(protoMsgReflectType):
		elemType = typector.FQNToProtoType(protoFQNFromGoType(et))
	case et.Implements(protoEnumReflectType):
		elemType = typector.FQNToEnumType(enumFQNFromGoType(et))
	default:
		return gcv{}, fmt.Errorf("%w: %T", ErrUnsupportedType, v)
	}
	if rv.IsNil() {
		return gcvctor.NullArrayOf(elemType), nil
	}
	elems := make([]gcv, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		e, err := encodeValue(cfg, rv.Index(i).Interface())
		if err != nil {
			return gcv{}, &gcvctor.ArrayElementError{Index: i, Err: err}
		}
		elems[i] = e
	}
	return gcvctor.ArrayValueOf(elemType, elems...)
}

// validateNumeric checks GoogleSQL NUMERIC bounds (precision 38, scale 9)
// with the same algorithm as the client's validateNumeric, applied under
// NumericError handling (this package's default; see
// [WithLossOfPrecisionHandling]): render
// with one extra fractional digit so an over-scale value survives rounding,
// then count the digits of each component.
func validateNumeric(cfg encodeConfig, r *big.Rat) error {
	if cfg.numericRounding() || r == nil {
		return nil
	}
	rendered := strings.TrimPrefix(r.FloatString(spanner.NumericScaleDigits+1), "-")
	dot := strings.IndexByte(rendered, '.')
	whole := rendered[:dot]
	frac := strings.TrimRight(rendered[dot+1:], "0")
	if len(frac) > spanner.NumericScaleDigits {
		return fmt.Errorf("%w: NUMERIC scale exceeds %d digits", ErrNumericOutOfRange, spanner.NumericScaleDigits)
	}
	if maxWhole := spanner.NumericPrecisionDigits - spanner.NumericScaleDigits; len(whole) > maxWhole {
		return fmt.Errorf("%w: NUMERIC whole component has %d digits, exceeding %d", ErrNumericOutOfRange, len(whole), maxWhole)
	}
	return nil
}
