package spanenc_test

import (
	"fmt"
	"os"

	"cloud.google.com/go/spanner"
	"github.com/apstndb/spanvalue"
	"github.com/apstndb/spanvalue/writer"

	"github.com/apstndb/spanenc"
)

// ExampleStructColumns derives Read columns from the same struct used with
// Row.ToStruct, the use case of
// https://github.com/googleapis/google-cloud-go/issues/13800.
func ExampleStructColumns() {
	type Singer struct {
		SingerID  int64 `spanner:"SingerId"`
		FirstName string
		LastName  string
		Internal  string `spanner:"-"`
	}

	columns, err := spanenc.StructColumns[Singer]()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(columns)
	// client.Single().Read(ctx, "Singers", spanner.AllKeys(), columns) ...
	// Output: [SingerId FirstName LastName]
}

// ExampleValueOf encodes Go values with the client library's parameter
// semantics and formats them with spanvalue.
func ExampleValueOf() {
	for _, v := range []any{
		"foo",
		(*int64)(nil),
		[]string{"a", "b"},
	} {
		gcv, err := spanenc.ValueOf(v)
		if err != nil {
			fmt.Println(err)
			return
		}
		s, err := spanvalue.FormatColumnLiteral(gcv)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(s)
	}
	// Output:
	// "foo"
	// NULL
	// ["a", "b"]
}

// ExampleStructColumnsAndValues streams Go structs to CSV through
// spanvalue/writer.
func ExampleStructColumnsAndValues() {
	type Singer struct {
		SingerID  int64 `spanner:"SingerId"`
		FirstName string
	}

	names, values, err := spanenc.StructColumnsAndValues(Singer{SingerID: 1, FirstName: "Marc"})
	if err != nil {
		fmt.Println(err)
		return
	}
	w, err := writer.NewCSVWriter(os.Stdout, writer.WithColumnNames(names))
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := w.WriteValues(names, values); err != nil {
		fmt.Println(err)
		return
	}
	if err := w.Flush(); err != nil {
		fmt.Println(err)
		return
	}
	// Output:
	// SingerId,FirstName
	// 1,Marc
}

// ExampleStructColumns_tagOptions contrasts the two struct field listings:
// row-shaped helpers parse `;`-separated tag options, while STRUCT-typed
// values mirror the client's raw-tag encodeStruct, so options leak into
// STRUCT field names verbatim (deliberate client parity). Use row-shaped
// APIs for table rows; reserve struct values for actual STRUCT-typed
// parameters.
func ExampleStructColumns_tagOptions() {
	type Row struct {
		Gen string `spanner:"Name;readonly"`
	}

	columns, err := spanenc.StructColumns[Row]() // row-shaped: options parsed
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(columns)

	typ, err := spanenc.TypeFor[Row]() // STRUCT-shaped: raw tag, like the client
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(typ.GetStructType().GetFields()[0].GetName())
	// Output:
	// [Name]
	// Name;readonly
}

// ExampleMutationColumnsAndValues masks columns by name before building a
// plain cols/vals mutation, which the *Struct mutation constructors cannot
// express.
func ExampleMutationColumnsAndValues() {
	type Singer struct {
		SingerID  int64 `spanner:"SingerId"`
		FirstName string
		LastName  string
	}

	// Update only SingerId and LastName; the mask could equally be written
	// as an exclude list with WithoutColumns("FirstName").
	cols, vals, err := spanenc.MutationColumnsAndValues(
		Singer{SingerID: 1, FirstName: "Marc", LastName: "Richards"},
		spanenc.WithColumns("SingerId", "LastName"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = spanner.Update("Singers", cols, vals)
	fmt.Println(cols)
	fmt.Println(vals)
	// Output:
	// [SingerId LastName]
	// [1 Richards]
}

// ExampleRowEncoder_Rows renders a client-side (virtual) result set through
// the same *spanner.Row pipeline used for server query results: structs are
// encoded into real rows, then formatted with spanvalue row formatters.
func ExampleRowEncoder_Rows() {
	type variable struct {
		Name  string `spanner:"name"`
		Value string `spanner:"value"`
	}
	enc := spanenc.MustNewRowEncoder[variable]()

	items := []variable{
		{Name: "AUTOCOMMIT", Value: "TRUE"},
		{Name: "READONLY", Value: "FALSE"},
	}
	for row, err := range enc.Rows(items) {
		if err != nil {
			fmt.Println(err)
			return
		}
		columns, err := spanvalue.FormatRowSpannerCLICompatible(row)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(columns)
	}
	// Output:
	// [AUTOCOMMIT TRUE]
	// [READONLY FALSE]
}
