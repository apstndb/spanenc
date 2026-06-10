package spanenc_test

import (
	"errors"
	"testing"

	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/apstndb/spantype/typector"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/apstndb/spanenc"
)

func TestValuesFromSlice(t *testing.T) {
	t.Parallel()

	t.Run("int64", func(t *testing.T) {
		t.Parallel()
		typ, values, err := spanenc.ValuesFromSlice([]int64{1, 2})
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(typector.Int64(), typ, protocmp.Transform()); diff != "" {
			t.Errorf("type mismatch (-want +got):\n%s", diff)
		}
		want := []*structpb.Value{structpb.NewStringValue("1"), structpb.NewStringValue("2")}
		if diff := cmp.Diff(want, values, protocmp.Transform()); diff != "" {
			t.Errorf("values mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("nil slice keeps type", func(t *testing.T) {
		t.Parallel()
		typ, values, err := spanenc.ValuesFromSlice[string](nil)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(typector.String(), typ, protocmp.Transform()); diff != "" {
			t.Errorf("type mismatch (-want +got):\n%s", diff)
		}
		if values != nil {
			t.Errorf("values = %v, want nil", values)
		}
	})

	t.Run("nil pointer elements", func(t *testing.T) {
		t.Parallel()
		typ, values, err := spanenc.ValuesFromSlice([]*int64{nil})
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(typector.Int64(), typ, protocmp.Transform()); diff != "" {
			t.Errorf("type mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]*structpb.Value{structpb.NewNullValue()}, values, protocmp.Transform()); diff != "" {
			t.Errorf("values mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("interface element type rejected", func(t *testing.T) {
		t.Parallel()
		if _, _, err := spanenc.ValuesFromSlice([]any{"a", int64(1)}); !errors.Is(err, spanenc.ErrTypeNotInferable) {
			t.Errorf("error = %v, want ErrTypeNotInferable", err)
		}
	})

	t.Run("GCV element type rejected", func(t *testing.T) {
		t.Parallel()
		if _, _, err := spanenc.ValuesFromSlice([]spanner.GenericColumnValue{}); !errors.Is(err, spanenc.ErrTypeNotInferable) {
			t.Errorf("error = %v, want ErrTypeNotInferable", err)
		}
	})
}

func TestArrayValueFromSlice(t *testing.T) {
	t.Parallel()

	t.Run("values", func(t *testing.T) {
		t.Parallel()
		got, err := spanenc.ArrayValueFromSlice([]string{"a"})
		if err != nil {
			t.Fatal(err)
		}
		want := spanner.GenericColumnValue{
			Type:  typector.ElemCodeToArrayType(sppb.TypeCode_STRING),
			Value: structpb.NewListValue(&structpb.ListValue{Values: []*structpb.Value{structpb.NewStringValue("a")}}),
		}
		if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("nil is typed NULL ARRAY", func(t *testing.T) {
		t.Parallel()
		got, err := spanenc.ArrayValueFromSlice[string](nil)
		if err != nil {
			t.Fatal(err)
		}
		want := spanner.GenericColumnValue{
			Type:  typector.ElemCodeToArrayType(sppb.TypeCode_STRING),
			Value: structpb.NewNullValue(),
		}
		if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("empty is empty ARRAY", func(t *testing.T) {
		t.Parallel()
		got, err := spanenc.ArrayValueFromSlice([]string{})
		if err != nil {
			t.Fatal(err)
		}
		want := spanner.GenericColumnValue{
			Type:  typector.ElemCodeToArrayType(sppb.TypeCode_STRING),
			Value: structpb.NewListValue(&structpb.ListValue{}),
		}
		if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})
}
