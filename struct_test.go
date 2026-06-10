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

package spanenc_test

import (
	"errors"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/apstndb/spantype/typector"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/apstndb/spanenc"
)

type singer struct {
	SingerID  int64 `spanner:"SingerId"`
	Name      string
	Secret    string `spanner:"-"`
	unexposed bool   //nolint:unused // exercises unexported-field skipping
	Tags      []string
}

type timestamps struct {
	CreatedAt time.Time `spanner:"CreatedAt"`
}

// album embeds timestamps; the row-shaped listing flattens it like the
// client's mutation/ToStruct field listing.
type album struct {
	AlbumID int64 `spanner:"AlbumId"`
	timestamps
}

// albumOverride shadows the embedded CreatedAt with a shallower field,
// following Go's embedding rules as applied by the client's fields cache.
type albumOverride struct {
	timestamps
	CreatedAt string `spanner:"CreatedAt"`
}

func TestStructColumns(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		desc string
		got  func() ([]string, error)
		want []string
	}{
		{"tags and skip", func() ([]string, error) { return spanenc.StructColumns[singer]() }, []string{"SingerId", "Name", "Tags"}},
		{"pointer type", func() ([]string, error) { return spanenc.StructColumns[*singer]() }, []string{"SingerId", "Name", "Tags"}},
		{"embedded flattened", func() ([]string, error) { return spanenc.StructColumns[album]() }, []string{"AlbumId", "CreatedAt"}},
		{"embedded shadowed", func() ([]string, error) { return spanenc.StructColumns[albumOverride]() }, []string{"CreatedAt"}},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			got, err := tt.got()
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("columns mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("non-struct", func(t *testing.T) {
		t.Parallel()
		if _, err := spanenc.StructColumns[int](); !errors.Is(err, spanenc.ErrNotStruct) {
			t.Errorf("error = %v, want ErrNotStruct", err)
		}
	})
}

func TestRowTypeFor(t *testing.T) {
	t.Parallel()

	got, err := spanenc.RowTypeFor[singer]()
	if err != nil {
		t.Fatal(err)
	}
	want := &sppb.StructType{Fields: []*sppb.StructType_Field{
		typector.NameCodeToStructTypeField("SingerId", sppb.TypeCode_INT64),
		typector.NameCodeToStructTypeField("Name", sppb.TypeCode_STRING),
		typector.NameTypeToStructTypeField("Tags", typector.ElemCodeToArrayType(sppb.TypeCode_STRING)),
	}}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("RowTypeFor mismatch (-want +got):\n%s", diff)
	}
}

func TestStructColumnsAndValues(t *testing.T) {
	t.Parallel()

	names, values, err := spanenc.StructColumnsAndValues(singer{SingerID: 1, Name: "n", Secret: "s", Tags: nil})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"SingerId", "Name", "Tags"}, names); diff != "" {
		t.Errorf("names mismatch (-want +got):\n%s", diff)
	}
	want := []spanner.GenericColumnValue{
		{Type: typector.Int64(), Value: structpb.NewStringValue("1")},
		{Type: typector.String(), Value: structpb.NewStringValue("n")},
		{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: structpb.NewNullValue()},
	}
	if diff := cmp.Diff(want, values, protocmp.Transform()); diff != "" {
		t.Errorf("values mismatch (-want +got):\n%s", diff)
	}

	t.Run("nil pointer", func(t *testing.T) {
		t.Parallel()
		if _, _, err := spanenc.StructColumnsAndValues((*singer)(nil)); !errors.Is(err, spanenc.ErrNilStructPointer) {
			t.Errorf("error = %v, want ErrNilStructPointer", err)
		}
	})
}

func TestMutationColumnsAndValues(t *testing.T) {
	t.Parallel()

	in := &singer{SingerID: 1, Name: "n", Tags: []string{"a"}}
	cols, vals, err := spanenc.MutationColumnsAndValues(in)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"SingerId", "Name", "Tags"}, cols); diff != "" {
		t.Errorf("cols mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{int64(1), "n", []string{"a"}}, vals); diff != "" {
		t.Errorf("vals mismatch (-want +got):\n%s", diff)
	}

	// The cols/vals pair feeds the client's plain mutation constructors,
	// optionally masked by column name.
	masked := make(map[string]bool, 1)
	masked["Tags"] = true
	var mcols []string
	var mvals []any
	for i, c := range cols {
		if masked[c] {
			continue
		}
		mcols = append(mcols, c)
		mvals = append(mvals, vals[i])
	}
	if m := spanner.Update("Singers", mcols, mvals); m == nil {
		t.Error("Update returned nil mutation")
	}

	t.Run("nil pointer", func(t *testing.T) {
		t.Parallel()
		if _, _, err := spanenc.MutationColumnsAndValues((*singer)(nil)); !errors.Is(err, spanenc.ErrNilStructPointer) {
			t.Errorf("error = %v, want ErrNilStructPointer", err)
		}
	})
	t.Run("non-struct", func(t *testing.T) {
		t.Parallel()
		if _, _, err := spanenc.MutationColumnsAndValues(42); !errors.Is(err, spanenc.ErrNotStruct) {
			t.Errorf("error = %v, want ErrNotStruct", err)
		}
	})
	t.Run("nil input", func(t *testing.T) {
		t.Parallel()
		if _, _, err := spanenc.MutationColumnsAndValues(nil); !errors.Is(err, spanenc.ErrNotStruct) {
			t.Errorf("error = %v, want ErrNotStruct", err)
		}
	})
}

func TestMutationMap(t *testing.T) {
	t.Parallel()

	got, err := spanenc.MutationMap(singer{SingerID: 1, Name: "n"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"SingerId": int64(1), "Name": "n", "Tags": []string(nil)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MutationMap mismatch (-want +got):\n%s", diff)
	}

	t.Run("same-name fields annihilate", func(t *testing.T) {
		t.Parallel()
		// Two same-depth fields with the same tagged name are dropped by the
		// fields cache (Go embedding-style annihilation), matching the
		// client's mutation field listing.
		type dup struct {
			A string `spanner:"X"`
			B string `spanner:"X"`
			C string
		}
		got, err := spanenc.MutationMap(dup{C: "c"})
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(map[string]any{"C": "c"}, got); diff != "" {
			t.Errorf("MutationMap mismatch (-want +got):\n%s", diff)
		}
	})
}
