// Copyright 2026 apstndb
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package spanenc

import (
	"database/sql"
	"fmt"
	"math/big"
	"reflect"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/apstndb/spantype/typector"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// exactGoTypes maps the Go types handled by concrete cases of the client's
// encodeValue type switch to constructors of their Spanner types. The
// constructor indirection avoids sharing mutable *sppb.Type values between
// callers. Keep this table in sync with the encodeValue cases mirrored in
// encode.go.
var exactGoTypes = map[reflect.Type]func() *sppb.Type{
	reflect.TypeFor[spanner.Interval]():       typector.Interval,
	reflect.TypeFor[[]spanner.Interval]():     arrayTypeCtor(typector.Interval),
	reflect.TypeFor[spanner.NullInterval]():   typector.Interval,
	reflect.TypeFor[[]spanner.NullInterval](): arrayTypeCtor(typector.Interval),

	reflect.TypeFor[string]():               typector.String,
	reflect.TypeFor[spanner.NullString]():   typector.String,
	reflect.TypeFor[sql.NullString]():       typector.String,
	reflect.TypeFor[[]string]():             arrayTypeCtor(typector.String),
	reflect.TypeFor[[]spanner.NullString](): arrayTypeCtor(typector.String),
	reflect.TypeFor[*string]():              typector.String,
	reflect.TypeFor[[]*string]():            arrayTypeCtor(typector.String),

	reflect.TypeFor[[]byte]():   typector.Bytes,
	reflect.TypeFor[[][]byte](): arrayTypeCtor(typector.Bytes),

	reflect.TypeFor[int]():                 typector.Int64,
	reflect.TypeFor[[]int]():               arrayTypeCtor(typector.Int64),
	reflect.TypeFor[int64]():               typector.Int64,
	reflect.TypeFor[[]int64]():             arrayTypeCtor(typector.Int64),
	reflect.TypeFor[spanner.NullInt64]():   typector.Int64,
	reflect.TypeFor[[]spanner.NullInt64](): arrayTypeCtor(typector.Int64),
	reflect.TypeFor[*int64]():              typector.Int64,
	reflect.TypeFor[[]*int64]():            arrayTypeCtor(typector.Int64),

	reflect.TypeFor[bool]():               typector.Bool,
	reflect.TypeFor[[]bool]():             arrayTypeCtor(typector.Bool),
	reflect.TypeFor[spanner.NullBool]():   typector.Bool,
	reflect.TypeFor[[]spanner.NullBool](): arrayTypeCtor(typector.Bool),
	reflect.TypeFor[*bool]():              typector.Bool,
	reflect.TypeFor[[]*bool]():            arrayTypeCtor(typector.Bool),

	reflect.TypeFor[float64]():               typector.Float64,
	reflect.TypeFor[[]float64]():             arrayTypeCtor(typector.Float64),
	reflect.TypeFor[spanner.NullFloat64]():   typector.Float64,
	reflect.TypeFor[[]spanner.NullFloat64](): arrayTypeCtor(typector.Float64),
	reflect.TypeFor[*float64]():              typector.Float64,
	reflect.TypeFor[[]*float64]():            arrayTypeCtor(typector.Float64),

	reflect.TypeFor[float32]():               typector.Float32,
	reflect.TypeFor[[]float32]():             arrayTypeCtor(typector.Float32),
	reflect.TypeFor[spanner.NullFloat32]():   typector.Float32,
	reflect.TypeFor[[]spanner.NullFloat32](): arrayTypeCtor(typector.Float32),
	reflect.TypeFor[*float32]():              typector.Float32,
	reflect.TypeFor[[]*float32]():            arrayTypeCtor(typector.Float32),

	reflect.TypeFor[big.Rat]():               typector.Numeric,
	reflect.TypeFor[[]big.Rat]():             arrayTypeCtor(typector.Numeric),
	reflect.TypeFor[spanner.NullNumeric]():   typector.Numeric,
	reflect.TypeFor[[]spanner.NullNumeric](): arrayTypeCtor(typector.Numeric),
	reflect.TypeFor[*big.Rat]():              typector.Numeric,
	reflect.TypeFor[[]*big.Rat]():            arrayTypeCtor(typector.Numeric),

	reflect.TypeFor[spanner.PGNumeric]():   typector.PGNumeric,
	reflect.TypeFor[[]spanner.PGNumeric](): arrayTypeCtor(typector.PGNumeric),

	reflect.TypeFor[spanner.NullJSON]():   typector.JSON,
	reflect.TypeFor[[]spanner.NullJSON](): arrayTypeCtor(typector.JSON),
	reflect.TypeFor[spanner.PGJsonB]():    typector.PGJSONB,
	reflect.TypeFor[[]spanner.PGJsonB]():  arrayTypeCtor(typector.PGJSONB),

	reflect.TypeFor[time.Time]():          typector.Timestamp,
	reflect.TypeFor[[]time.Time]():        arrayTypeCtor(typector.Timestamp),
	reflect.TypeFor[spanner.NullTime]():   typector.Timestamp,
	reflect.TypeFor[[]spanner.NullTime](): arrayTypeCtor(typector.Timestamp),
	reflect.TypeFor[*time.Time]():         typector.Timestamp,
	reflect.TypeFor[[]*time.Time]():       arrayTypeCtor(typector.Timestamp),

	reflect.TypeFor[civil.Date]():         typector.Date,
	reflect.TypeFor[[]civil.Date]():       arrayTypeCtor(typector.Date),
	reflect.TypeFor[spanner.NullDate]():   typector.Date,
	reflect.TypeFor[[]spanner.NullDate](): arrayTypeCtor(typector.Date),
	reflect.TypeFor[*civil.Date]():        typector.Date,
	reflect.TypeFor[[]*civil.Date]():      arrayTypeCtor(typector.Date),

	reflect.TypeFor[uuid.UUID]():          typector.UUID,
	reflect.TypeFor[[]uuid.UUID]():        arrayTypeCtor(typector.UUID),
	reflect.TypeFor[[]*uuid.UUID]():       arrayTypeCtor(typector.UUID),
	reflect.TypeFor[spanner.NullUUID]():   typector.UUID,
	reflect.TypeFor[[]spanner.NullUUID](): arrayTypeCtor(typector.UUID),
	reflect.TypeFor[uuid.NullUUID]():      typector.UUID,
	reflect.TypeFor[*uuid.UUID]():         typector.UUID,
	reflect.TypeFor[*spanner.NullUUID]():  typector.UUID,
	reflect.TypeFor[*uuid.NullUUID]():     typector.UUID,
}

// notInferableGoTypes are handled by concrete cases of the client's
// encodeValue but carry value-dependent (or absent) type information, so a
// type-only derivation is impossible.
var notInferableGoTypes = map[reflect.Type]bool{
	reflect.TypeFor[spanner.GenericColumnValue]():   true,
	reflect.TypeFor[[]spanner.GenericColumnValue](): true,
	reflect.TypeFor[spanner.NullProtoMessage]():     true,
	reflect.TypeFor[spanner.NullProtoEnum]():        true,
}

func arrayTypeCtor(elem func() *sppb.Type) func() *sppb.Type {
	return func() *sppb.Type { return typector.ElemTypeToArrayType(elem()) }
}

var (
	encoderReflectType   = reflect.TypeFor[spanner.Encoder]()
	protoMsgReflectType  = reflect.TypeFor[proto.Message]()
	protoEnumReflectType = reflect.TypeFor[protoreflect.Enum]()
)

// TypeFor returns the [sppb.Type] that [ValueOf] would produce for a
// non-NULL value of Go type T, following the Cloud Spanner client library
// encoding semantics. See [TypeFromGoType].
func TypeFor[T any]() (*sppb.Type, error) {
	return TypeFromGoType(reflect.TypeFor[T]())
}

// TypeFromGoType returns the [sppb.Type] that [ValueOf] produces for values
// of the given Go type, following the Cloud Spanner client library encoding
// semantics (encodeValue in cloud.google.com/go/spanner).
//
// It returns [ErrTypeNotInferable] for Go types whose Spanner type depends on
// the value rather than the type: interface types,
// [cloud.google.com/go/spanner.Encoder] implementations,
// [cloud.google.com/go/spanner.GenericColumnValue],
// [cloud.google.com/go/spanner.NullProtoMessage], and
// [cloud.google.com/go/spanner.NullProtoEnum]. It returns
// [ErrUnsupportedType] for Go types the client library cannot encode.
func TypeFromGoType(t reflect.Type) (*sppb.Type, error) {
	if t == nil {
		return nil, ErrUntypedNil
	}
	if ctor, ok := exactGoTypes[t]; ok {
		return ctor(), nil
	}
	if notInferableGoTypes[t] {
		return nil, fmt.Errorf("%w: %v", ErrTypeNotInferable, t)
	}
	// The client checks Encoder before the proto/enum encoding inside those
	// cases and before the custom-type fallback in the default case, so any
	// non-exact type implementing Encoder is encoded via EncodeSpanner, whose
	// result type is value-dependent.
	if t.Implements(encoderReflectType) {
		return nil, fmt.Errorf("%w: %v implements spanner.Encoder", ErrTypeNotInferable, t)
	}
	if t.Implements(protoEnumReflectType) {
		return typector.FQNToEnumType(enumFQNFromGoType(t)), nil
	}
	if t.Implements(protoMsgReflectType) {
		return typector.FQNToProtoType(protoFQNFromGoType(t)), nil
	}
	if base, ok := customBaseGoType(t); ok {
		return TypeFromGoType(base)
	}
	switch {
	case t.Kind() == reflect.Struct,
		t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct:
		return structTypeFromGoType(t)
	case t.Kind() == reflect.Slice:
		et := t.Elem()
		switch {
		case et.Implements(protoEnumReflectType):
			return typector.ElemTypeToArrayType(typector.FQNToEnumType(enumFQNFromGoType(et))), nil
		case et.Implements(protoMsgReflectType):
			return typector.ElemTypeToArrayType(typector.FQNToProtoType(protoFQNFromGoType(et))), nil
		case et.Kind() == reflect.Struct,
			et.Kind() == reflect.Pointer && et.Elem().Kind() == reflect.Struct:
			st, err := structTypeFromGoType(et)
			if err != nil {
				return nil, err
			}
			return typector.ElemTypeToArrayType(st), nil
		}
	case t.Kind() == reflect.Interface:
		return nil, fmt.Errorf("%w: interface type %v", ErrTypeNotInferable, t)
	}
	return nil, fmt.Errorf("%w: %v", ErrUnsupportedType, t)
}

// enumFQNFromGoType returns the fully qualified protobuf enum name for a Go
// type implementing protoreflect.Enum, mirroring the client's handling of
// typed nil enum pointers (descriptor taken from the pointed-to zero value).
func enumFQNFromGoType(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	e := reflect.Zero(t).Interface().(protoreflect.Enum)
	return string(e.Descriptor().FullName())
}

// protoFQNFromGoType returns the fully qualified protobuf message name for a
// Go type implementing proto.Message. Generated message types expose their
// descriptor on a typed nil receiver, matching the client's encodeProtoArray.
func protoFQNFromGoType(t reflect.Type) string {
	m := reflect.Zero(t).Interface().(proto.Message)
	return string(m.ProtoReflect().Descriptor().FullName())
}

// structTypeFromGoType derives the STRUCT type of a Go struct (or pointer to
// struct) following the client's encodeStruct: fields in declaration order,
// embedded fields rejected, unexported fields skipped, and the `spanner` tag
// looked up with Lookup so `spanner:""` yields an unnamed field.
func structTypeFromGoType(t reflect.Type) (*sppb.Type, error) {
	if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedType, t)
	}
	fields := make([]*sppb.StructType_Field, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Anonymous {
			return nil, fmt.Errorf("%w: field %s of %v", ErrEmbeddedStructField, sf.Name, t)
		}
		if sf.PkgPath != "" { // unexported
			continue
		}
		fname, ok := sf.Tag.Lookup("spanner")
		if !ok {
			fname = sf.Name
		}
		ft, err := TypeFromGoType(sf.Type)
		if err != nil {
			return nil, fmt.Errorf("struct field %d (%q): %w", i, fname, err)
		}
		fields = append(fields, typector.NameTypeToStructTypeField(fname, ft))
	}
	return typector.StructTypeFieldsToStructType(fields), nil
}

// customBaseGoType mirrors the encoding half of the client's
// getDecodableSpannerType: it reports whether t is a named variant of a
// supported base type and returns that base type. Slices map to slices of
// the base element type; the per-element conversion happens in encode.go.
func customBaseGoType(t reflect.Type) (reflect.Type, bool) {
	switch t.Kind() {
	case reflect.Array:
		if t.ConvertibleTo(reflect.TypeFor[uuid.UUID]()) {
			return reflect.TypeFor[uuid.UUID](), true
		}
	case reflect.String:
		return reflect.TypeFor[string](), true
	case reflect.Int64:
		return reflect.TypeFor[int64](), true
	case reflect.Bool:
		return reflect.TypeFor[bool](), true
	case reflect.Float32:
		return reflect.TypeFor[float32](), true
	case reflect.Float64:
		return reflect.TypeFor[float64](), true
	case reflect.Pointer:
		// Upstream checks ConvertibleTo against struct types on a pointer
		// kind; pointer-to-struct conversions never hold in Go, so this
		// branch never matches. Kept for parity with getDecodableSpannerType.
		return nil, false
	case reflect.Struct:
		for _, base := range customStructBases {
			if t.ConvertibleTo(base) {
				return base, true
			}
		}
	case reflect.Slice:
		eb, ok := customSliceElemBase(t.Elem())
		if !ok {
			return nil, false
		}
		return reflect.SliceOf(eb), true
	}
	return nil, false
}

// customStructBases lists the struct base types accepted by the client's
// getDecodableSpannerType for kind Struct, in the upstream evaluation order.
var customStructBases = []reflect.Type{
	reflect.TypeFor[big.Rat](),
	reflect.TypeFor[time.Time](),
	reflect.TypeFor[civil.Date](),
	reflect.TypeFor[spanner.Interval](),
	reflect.TypeFor[spanner.NullString](),
	reflect.TypeFor[spanner.NullInt64](),
	reflect.TypeFor[spanner.NullBool](),
	reflect.TypeFor[spanner.NullFloat64](),
	reflect.TypeFor[spanner.NullFloat32](),
	reflect.TypeFor[spanner.NullTime](),
	reflect.TypeFor[spanner.NullDate](),
	reflect.TypeFor[spanner.NullNumeric](),
	reflect.TypeFor[spanner.NullJSON](),
	reflect.TypeFor[spanner.NullUUID](),
	reflect.TypeFor[spanner.PGNumeric](),
	reflect.TypeFor[spanner.PGJsonB](),
	reflect.TypeFor[spanner.NullInterval](),
}

// customSliceStructBases lists the struct base types accepted for slice
// elements by getDecodableSpannerType. Unlike customStructBases it does not
// include Interval or NullInterval, mirroring upstream.
var customSliceStructBases = []reflect.Type{
	reflect.TypeFor[big.Rat](),
	reflect.TypeFor[time.Time](),
	reflect.TypeFor[civil.Date](),
	reflect.TypeFor[spanner.NullString](),
	reflect.TypeFor[spanner.NullInt64](),
	reflect.TypeFor[spanner.NullBool](),
	reflect.TypeFor[spanner.NullFloat64](),
	reflect.TypeFor[spanner.NullFloat32](),
	reflect.TypeFor[spanner.NullTime](),
	reflect.TypeFor[spanner.NullDate](),
	reflect.TypeFor[spanner.NullNumeric](),
	reflect.TypeFor[spanner.NullJSON](),
	reflect.TypeFor[spanner.PGNumeric](),
	reflect.TypeFor[spanner.PGJsonB](),
	reflect.TypeFor[spanner.NullUUID](),
}

// customSliceElemBase mirrors the kind Slice arm of getDecodableSpannerType.
func customSliceElemBase(et reflect.Type) (reflect.Type, bool) {
	switch et.Kind() {
	case reflect.String:
		return reflect.TypeFor[string](), true
	case reflect.Uint8:
		// []uint8 variants map to BYTES ([]byte), not ARRAY<...>.
		return reflect.TypeFor[byte](), true
	case reflect.Int64:
		return reflect.TypeFor[int64](), true
	case reflect.Bool:
		return reflect.TypeFor[bool](), true
	case reflect.Float64:
		return reflect.TypeFor[float64](), true
	case reflect.Float32:
		return reflect.TypeFor[float32](), true
	case reflect.Array:
		if et.ConvertibleTo(reflect.TypeFor[uuid.UUID]()) {
			return reflect.TypeFor[uuid.UUID](), true
		}
	case reflect.Struct:
		for _, base := range customSliceStructBases {
			if et.ConvertibleTo(base) {
				return base, true
			}
		}
	case reflect.Slice:
		// The only supported slice-of-slice base is [][]byte.
		if et.Elem().Kind() == reflect.Uint8 {
			return reflect.TypeFor[[]byte](), true
		}
	}
	return nil, false
}
