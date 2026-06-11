package spanenc_test

import (
	"errors"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/apstndb/spantype/typector"
	"github.com/apstndb/spanvalue"
	"github.com/apstndb/spanvalue/writer"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/apstndb/spanenc"
)

func TestRowEncoder(t *testing.T) {
	t.Parallel()

	enc, err := spanenc.NewRowEncoder[singer]()
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff([]string{"SingerId", "Name", "Tags"}, enc.Columns()); diff != "" {
		t.Errorf("Columns mismatch (-want +got):\n%s", diff)
	}

	rowType, err := enc.RowType()
	if err != nil {
		t.Fatal(err)
	}
	wantRowType := &sppb.StructType{Fields: []*sppb.StructType_Field{
		typector.NameCodeToStructTypeField("SingerId", sppb.TypeCode_INT64),
		typector.NameCodeToStructTypeField("Name", sppb.TypeCode_STRING),
		typector.NameTypeToStructTypeField("Tags", typector.ElemCodeToArrayType(sppb.TypeCode_STRING)),
	}}
	if diff := cmp.Diff(wantRowType, rowType, protocmp.Transform()); diff != "" {
		t.Errorf("RowType mismatch (-want +got):\n%s", diff)
	}

	values, err := enc.Values(singer{SingerID: 1, Name: "n"})
	if err != nil {
		t.Fatal(err)
	}
	wantValues := []spanner.GenericColumnValue{
		{Type: typector.Int64(), Value: structpb.NewStringValue("1")},
		{Type: typector.String(), Value: structpb.NewStringValue("n")},
		{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: structpb.NewNullValue()},
	}
	if diff := cmp.Diff(wantValues, values, protocmp.Transform()); diff != "" {
		t.Errorf("Values mismatch (-want +got):\n%s", diff)
	}

	t.Run("agrees with StructColumnsAndValues", func(t *testing.T) {
		t.Parallel()
		in := singer{SingerID: 2, Name: "x", Tags: []string{"t"}}
		names, want, err := spanenc.StructColumnsAndValues(in)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(names, enc.Columns()); diff != "" {
			t.Errorf("Columns disagree (-StructColumnsAndValues +RowEncoder):\n%s", diff)
		}
		got, err := enc.Values(in)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
			t.Errorf("Values disagree (-StructColumnsAndValues +RowEncoder):\n%s", diff)
		}
	})
}

func TestRowEncoderMaskAndErrors(t *testing.T) {
	t.Parallel()

	t.Run("mask applies to all outputs", func(t *testing.T) {
		t.Parallel()
		enc, err := spanenc.NewRowEncoder[singer](spanenc.WithoutColumns("Tags"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]string{"SingerId", "Name"}, enc.Columns()); diff != "" {
			t.Errorf("Columns mismatch (-want +got):\n%s", diff)
		}
		rowType, err := enc.RowType()
		if err != nil {
			t.Fatal(err)
		}
		if len(rowType.GetFields()) != 2 {
			t.Errorf("RowType fields = %v, want 2", rowType.GetFields())
		}
		values, err := enc.Values(singer{SingerID: 1, Name: "n", Tags: []string{"dropped"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(values) != 2 {
			t.Errorf("Values = %v, want 2 elements", values)
		}
	})

	t.Run("include mask may name read-only columns", func(t *testing.T) {
		t.Parallel()
		enc, err := spanenc.NewRowEncoder[readOnlyRow](spanenc.WithColumns("Gen"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]string{"Gen"}, enc.Columns()); diff != "" {
			t.Errorf("Columns mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("invalid mask", func(t *testing.T) {
		t.Parallel()
		if _, err := spanenc.NewRowEncoder[singer](spanenc.WithColumns("Nope")); !errors.Is(err, spanenc.ErrInvalidColumnMask) {
			t.Errorf("error = %v, want ErrInvalidColumnMask", err)
		}
	})

	t.Run("MustNewRowEncoder", func(t *testing.T) {
		t.Parallel()
		if got := spanenc.MustNewRowEncoder[singer]().Columns(); len(got) != 3 {
			t.Errorf("Columns = %v, want 3 names", got)
		}
		defer func() {
			if recover() == nil {
				t.Error("MustNewRowEncoder[int]: want panic, got none")
			}
		}()
		spanenc.MustNewRowEncoder[int]()
	})

	t.Run("non-struct", func(t *testing.T) {
		t.Parallel()
		if _, err := spanenc.NewRowEncoder[int](); !errors.Is(err, spanenc.ErrNotStruct) {
			t.Errorf("error = %v, want ErrNotStruct", err)
		}
	})

	t.Run("nil pointer row", func(t *testing.T) {
		t.Parallel()
		enc, err := spanenc.NewRowEncoder[*singer]()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := enc.Values(nil); !errors.Is(err, spanenc.ErrNilStructPointer) {
			t.Errorf("error = %v, want ErrNilStructPointer", err)
		}
		values, err := enc.Values(&singer{SingerID: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(values) != 3 {
			t.Errorf("Values = %v, want 3 elements", values)
		}
	})
}

// TestRowEncoderWriterIntegration streams a virtual result set built from Go
// structs through spanvalue/writer, the use case surveyed from spanner-mycli
// and spannersh.
func TestRowEncoderWriterIntegration(t *testing.T) {
	t.Parallel()

	type variable struct {
		Name  string `spanner:"name"`
		Value string `spanner:"value"`
	}
	enc, err := spanenc.NewRowEncoder[variable]()
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := enc.ResultSetMetadata()
	if err != nil {
		t.Fatal(err)
	}

	var sb strings.Builder
	w, err := writer.NewCSVWriter(&sb, writer.DelimitedGCVExportOptions(
		metadata,
		spanvalue.SimpleFormatConfig(),
		spanvalue.IndexedUnnamedFieldNamer,
	)...)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []variable{{"AUTOCOMMIT", "TRUE"}, {"READONLY", "FALSE"}} {
		values, err := enc.Values(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteGCVs(values); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "name,value\nAUTOCOMMIT,TRUE\nREADONLY,FALSE\n"
	if diff := cmp.Diff(want, sb.String()); diff != "" {
		t.Errorf("CSV mismatch (-want +got):\n%s", diff)
	}
}

func TestRowEncoderRow(t *testing.T) {
	t.Parallel()

	enc, err := spanenc.NewRowEncoder[singer]()
	if err != nil {
		t.Fatal(err)
	}

	in := singer{SingerID: 1, Name: "n"} // Tags nil = typed NULL ARRAY<STRING>
	row, err := enc.Row(in)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(enc.Columns(), row.ColumnNames()); diff != "" {
		t.Errorf("ColumnNames mismatch (-Columns +Row):\n%s", diff)
	}

	// The row must carry exactly the GCVs Values produced, including the
	// typed NULL: spanner.NewRow passes GenericColumnValue through unchanged.
	want, err := enc.Values(in)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]spanner.GenericColumnValue, row.Size())
	for i := range row.Size() {
		if err := row.Column(i, &got[i]); err != nil {
			t.Fatal(err)
		}
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("row GCVs mismatch (-Values +Row):\n%s", diff)
	}

	// Decode round-trip through the real client's typed decoding.
	var (
		id   int64
		name string
		tags []string
	)
	if err := row.Columns(&id, &name, &tags); err != nil {
		t.Fatal(err)
	}
	if id != 1 || name != "n" || tags != nil {
		t.Errorf("decoded = (%d, %q, %v), want (1, \"n\", nil)", id, name, tags)
	}

	t.Run("nil pointer row", func(t *testing.T) {
		t.Parallel()
		enc, err := spanenc.NewRowEncoder[*singer]()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := enc.Row(nil); !errors.Is(err, spanenc.ErrNilStructPointer) {
			t.Errorf("error = %v, want ErrNilStructPointer", err)
		}
	})
}

func TestRowEncoderRows(t *testing.T) {
	t.Parallel()

	t.Run("yields all rows", func(t *testing.T) {
		t.Parallel()
		enc, err := spanenc.NewRowEncoder[singer]()
		if err != nil {
			t.Fatal(err)
		}
		var formatted [][]string
		for row, err := range enc.Rows([]singer{{SingerID: 1, Name: "a"}, {SingerID: 2, Name: "b"}}) {
			if err != nil {
				t.Fatal(err)
			}
			cols, err := spanvalue.FormatRowSpannerCLICompatible(row)
			if err != nil {
				t.Fatal(err)
			}
			formatted = append(formatted, cols)
		}
		want := [][]string{{"1", "a", "NULL"}, {"2", "b", "NULL"}}
		if diff := cmp.Diff(want, formatted); diff != "" {
			t.Errorf("rows mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("lazy encoding allows early stop before a failing item", func(t *testing.T) {
		t.Parallel()
		type anyField struct {
			V any
		}
		enc, err := spanenc.NewRowEncoder[anyField]()
		if err != nil {
			t.Fatal(err)
		}
		items := []anyField{{V: int64(1)}, {V: nil}} // second item fails with ErrUntypedNil
		var seen int
		for _, err := range enc.Rows(items) {
			if err != nil {
				t.Fatalf("unexpected error before stop: %v", err)
			}
			seen++
			break
		}
		if seen != 1 {
			t.Errorf("seen = %d, want 1", seen)
		}
	})

	t.Run("stops after yielding the first encode error", func(t *testing.T) {
		t.Parallel()
		type anyField struct {
			V any
		}
		enc, err := spanenc.NewRowEncoder[anyField]()
		if err != nil {
			t.Fatal(err)
		}
		items := []anyField{{V: int64(1)}, {V: nil}, {V: int64(3)}}
		var rows, errs int
		var gotErr error
		for row, err := range enc.Rows(items) {
			if err != nil {
				errs++
				gotErr = err
				if row != nil {
					t.Error("row should be nil on error")
				}
				continue
			}
			rows++
		}
		if rows != 1 || errs != 1 {
			t.Errorf("rows = %d, errs = %d, want 1 row then 1 error", rows, errs)
		}
		if !errors.Is(gotErr, spanenc.ErrUntypedNil) {
			t.Errorf("error = %v, want ErrUntypedNil", gotErr)
		}
	})
}
