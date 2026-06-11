package spanenc_test

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/apstndb/spantype/typector"
	"github.com/apstndb/spanvalue/gcvctor"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/apstndb/spanenc"
)

// uint32AsInt64 is the canonical use case: a built-in Go type the client's
// encodeValue does not support.
func uint32AsInt64(v uint32) (spanner.GenericColumnValue, error) {
	return gcvctor.Int64Value(int64(v)), nil
}

// uint64AsInt64 needs a range check: values above MaxInt64 have no INT64
// representation.
func uint64AsInt64(v uint64) (spanner.GenericColumnValue, error) {
	if v > math.MaxInt64 {
		return spanner.GenericColumnValue{}, fmt.Errorf("uint64 %d overflows INT64", v)
	}
	return gcvctor.Int64Value(int64(v)), nil
}

func TestWithValueEncoder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    any
		opts []spanenc.EncodeOption
		want spanner.GenericColumnValue
	}{
		{
			"uint32 scalar",
			uint32(7),
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(uint32AsInt64)},
			spanner.GenericColumnValue{Type: typector.Int64(), Value: str("7")},
		},
		{
			"uint64 in range",
			uint64(8),
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(uint64AsInt64)},
			spanner.GenericColumnValue{Type: typector.Int64(), Value: str("8")},
		},
		{
			"time.Duration as INT64 nanoseconds",
			2 * time.Second,
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(func(d time.Duration) (spanner.GenericColumnValue, error) {
				return gcvctor.Int64Value(d.Nanoseconds()), nil
			})},
			spanner.GenericColumnValue{Type: typector.Int64(), Value: str(strconv.FormatInt(2e9, 10))},
		},
		{
			"overrides built-in string handling",
			"abc",
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(func(s string) (spanner.GenericColumnValue, error) {
				return gcvctor.StringValue(strings.ToUpper(s)), nil
			})},
			spanner.GenericColumnValue{Type: typector.String(), Value: str("ABC")},
		},
		{
			"overrides a spanner.Encoder implementation",
			myEncoder{v: "ignored"},
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(func(myEncoder) (spanner.GenericColumnValue, error) {
				return gcvctor.StringValue("registered"), nil
			})},
			spanner.GenericColumnValue{Type: typector.String(), Value: str("registered")},
		},
		{
			"overrides the named-type conversion",
			myString("x"),
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(func(myString) (spanner.GenericColumnValue, error) {
				return gcvctor.StringValue("custom"), nil
			})},
			spanner.GenericColumnValue{Type: typector.String(), Value: str("custom")},
		},
		{
			// Pins the documented hazard: a time.Time registration bypasses
			// the CommitTimestamp sentinel detection of the mirror.
			"time.Time registration bypasses CommitTimestamp sentinel",
			spanner.CommitTimestamp,
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(func(time.Time) (spanner.GenericColumnValue, error) {
				return gcvctor.StringValue("intercepted"), nil
			})},
			spanner.GenericColumnValue{Type: typector.String(), Value: str("intercepted")},
		},
		{
			"last registration wins",
			uint32(1),
			[]spanenc.EncodeOption{
				spanenc.WithValueEncoder(func(uint32) (spanner.GenericColumnValue, error) {
					return gcvctor.StringValue("first"), nil
				}),
				spanenc.WithValueEncoder(uint32AsInt64),
			},
			spanner.GenericColumnValue{Type: typector.Int64(), Value: str("1")},
		},
		{
			"fallthrough defers to the mirror",
			"plain",
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(func(s string) (spanner.GenericColumnValue, error) {
				if s == "special" {
					return gcvctor.StringValue("SPECIAL"), nil
				}
				return spanner.GenericColumnValue{}, spanenc.ErrFallthrough
			})},
			spanner.GenericColumnValue{Type: typector.String(), Value: str("plain")},
		},
		{
			"slice of registered type encodes per element",
			[]uint32{1, 2},
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(uint32AsInt64)},
			spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_INT64), Value: listValue(str("1"), str("2"))},
		},
		{
			"slice elements honor registration over the built-in case",
			[]string{"a"},
			[]spanenc.EncodeOption{spanenc.WithValueEncoder(func(s string) (spanner.GenericColumnValue, error) {
				return gcvctor.StringValue(strings.ToUpper(s)), nil
			})},
			spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: listValue(str("A"))},
		},
		{
			"nil slice with WithGoType is a typed NULL ARRAY",
			[]uint32(nil),
			[]spanenc.EncodeOption{
				spanenc.WithValueEncoder(uint32AsInt64),
				spanenc.WithGoType[uint32](typector.Int64()),
			},
			spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_INT64), Value: nullValue()},
		},
		{
			"empty slice with WithGoType is an empty ARRAY",
			[]uint32{},
			[]spanenc.EncodeOption{
				spanenc.WithValueEncoder(uint32AsInt64),
				spanenc.WithGoType[uint32](typector.Int64()),
			},
			spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_INT64), Value: listValue()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := spanenc.ValueOf(tt.v, tt.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ValueOf mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWithValueEncoderErrors(t *testing.T) {
	t.Parallel()

	t.Run("encoder error propagates", func(t *testing.T) {
		t.Parallel()
		_, err := spanenc.ValueOf(uint64(math.MaxUint64), spanenc.WithValueEncoder(uint64AsInt64))
		if err == nil || !strings.Contains(err.Error(), "overflows INT64") {
			t.Errorf("error = %v, want overflow error", err)
		}
	})

	t.Run("struct field error is wrapped with the field", func(t *testing.T) {
		t.Parallel()
		type row struct {
			N uint64 `spanner:"n"`
		}
		_, _, err := spanenc.StructColumnsAndValues(row{N: math.MaxUint64}, spanenc.WithValueEncoder(uint64AsInt64))
		var sfe *gcvctor.StructFieldError
		if !errors.As(err, &sfe) || sfe.Name != "n" {
			t.Errorf("error = %v, want StructFieldError for field n", err)
		}
	})

	t.Run("nil slice without WithGoType defers to the mirror", func(t *testing.T) {
		t.Parallel()
		if _, err := spanenc.ValueOf([]uint32(nil), spanenc.WithValueEncoder(uint32AsInt64)); !errors.Is(err, spanenc.ErrUnsupportedType) {
			t.Errorf("error = %v, want ErrUnsupportedType", err)
		}
		// Built-in element types keep their mirror behavior even when the
		// element encoder always falls through.
		got, err := spanenc.ValueOf([]string(nil), spanenc.WithValueEncoder(func(string) (spanner.GenericColumnValue, error) {
			return spanner.GenericColumnValue{}, spanenc.ErrFallthrough
		}))
		if err != nil {
			t.Fatal(err)
		}
		want := spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: nullValue()}
		if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
			t.Errorf("nil []string mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("slice element error is wrapped with the index", func(t *testing.T) {
		t.Parallel()
		_, err := spanenc.ValueOf([]uint64{1, math.MaxUint64}, spanenc.WithValueEncoder(uint64AsInt64))
		var aee *gcvctor.ArrayElementError
		if !errors.As(err, &aee) || aee.Index != 1 {
			t.Errorf("error = %v, want ArrayElementError at index 1", err)
		}
	})

	t.Run("interface type registration panics", func(t *testing.T) {
		t.Parallel()
		defer func() {
			if recover() == nil {
				t.Error("WithValueEncoder[error]: want panic, got none")
			}
		}()
		_, _ = spanenc.ValueOf("x", spanenc.WithValueEncoder(func(error) (spanner.GenericColumnValue, error) {
			return spanner.GenericColumnValue{}, nil
		}))
	})
}

// TestSliceHelpersWithGoType pins that ValuesFromSlice / ArrayValueFromSlice
// accept WithGoType + WithValueEncoder for element types that static
// inference alone rejects.
func TestSliceHelpersWithGoType(t *testing.T) {
	t.Parallel()

	opts := []spanenc.EncodeOption{
		spanenc.WithValueEncoder(uint32AsInt64),
		spanenc.WithGoType[uint32](typector.Int64()),
	}

	elemType, values, err := spanenc.ValuesFromSlice([]uint32{1, 2}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(typector.Int64(), elemType, protocmp.Transform()); diff != "" {
		t.Errorf("element type mismatch (-want +got):\n%s", diff)
	}
	if len(values) != 2 {
		t.Errorf("values = %v, want 2 elements", values)
	}

	got, err := spanenc.ArrayValueFromSlice([]uint32(nil), opts...)
	if err != nil {
		t.Fatal(err)
	}
	want := spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_INT64), Value: nullValue()}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("nil slice mismatch (-want +got):\n%s", diff)
	}

	// Without the options, static inference still rejects uint32.
	if _, _, err := spanenc.ValuesFromSlice([]uint32{1}); !errors.Is(err, spanenc.ErrUnsupportedType) {
		t.Errorf("error = %v, want ErrUnsupportedType", err)
	}
}
