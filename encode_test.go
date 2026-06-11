package spanenc_test

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"math"
	"math/big"
	"reflect"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/apstndb/spantype/typector"
	"github.com/apstndb/spanvalue/gcvctor"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/apstndb/spanenc"
)

// Custom (named) types exercising the client's getDecodableSpannerType
// fallback.
type (
	myString string
	myInt64  int64
	myInt    int // kind Int is NOT a supported custom base type upstream
	myTags   []string
	myBlob   []byte
	myTime   time.Time
	myUUID   [16]byte
)

// myEncoder exercises the spanner.Encoder path.
type myEncoder struct{ v string }

func (e myEncoder) EncodeSpanner() (any, error) { return e.v, nil }

// tuple is a struct encoded as a STRUCT value (encodeStruct semantics).
type tuple struct {
	ID     int64 `spanner:"Id"`
	Name   string
	hidden bool   //nolint:unused // exercises unexported-field skipping
	Blank  string `spanner:""`
}

func listValue(vs ...*structpb.Value) *structpb.Value {
	return structpb.NewListValue(&structpb.ListValue{Values: vs})
}

func nullValue() *structpb.Value { return structpb.NewNullValue() }

func str(s string) *structpb.Value { return structpb.NewStringValue(s) }

func TestValueOf(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.FixedZone("JST", 9*3600))
	date := civil.Date{Year: 2026, Month: 1, Day: 2}
	u := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	rat := big.NewRat(199, 2) // 99.5
	iv, err := spanner.ParseInterval("P1Y2M3DT4H5M6S")
	if err != nil {
		t.Fatal(err)
	}
	dur := durationpb.New(time.Second)
	durBytes, err := proto.Marshal(dur)
	if err != nil {
		t.Fatal(err)
	}

	// Blank carries `spanner:""`, which the client's encodeStruct treats as
	// an unnamed STRUCT field (tag Lookup, not Get).
	tupleType := typector.StructTypeFieldsToStructType([]*sppb.StructType_Field{
		typector.NameCodeToStructTypeField("Id", sppb.TypeCode_INT64),
		typector.NameCodeToStructTypeField("Name", sppb.TypeCode_STRING),
		typector.NameCodeToStructTypeField("", sppb.TypeCode_STRING),
	})

	for _, tt := range []struct {
		desc  string
		input any
		want  spanner.GenericColumnValue
	}{
		{"string", "foo", spanner.GenericColumnValue{Type: typector.String(), Value: str("foo")}},
		{"NullString valid", spanner.NullString{StringVal: "foo", Valid: true}, spanner.GenericColumnValue{Type: typector.String(), Value: str("foo")}},
		{"NullString invalid", spanner.NullString{}, spanner.GenericColumnValue{Type: typector.String(), Value: nullValue()}},
		{"sql.NullString valid", sql.NullString{String: "foo", Valid: true}, spanner.GenericColumnValue{Type: typector.String(), Value: str("foo")}},
		{"*string nil", (*string)(nil), spanner.GenericColumnValue{Type: typector.String(), Value: nullValue()}},
		{"[]string nil", []string(nil), spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: nullValue()}},
		{"[]string empty", []string{}, spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: listValue()}},
		{"[]string values", []string{"a", "b"}, spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: listValue(str("a"), str("b"))}},
		{"[]*string with nil", []*string{nil}, spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: listValue(nullValue())}},

		{"bytes", []byte("ab"), spanner.GenericColumnValue{Type: typector.Bytes(), Value: str(base64.StdEncoding.EncodeToString([]byte("ab")))}},
		{"bytes nil", []byte(nil), spanner.GenericColumnValue{Type: typector.Bytes(), Value: nullValue()}},
		{"[][]byte", [][]byte{[]byte("ab"), nil}, spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_BYTES), Value: listValue(str(base64.StdEncoding.EncodeToString([]byte("ab"))), nullValue())}},

		{"int", int(1), spanner.GenericColumnValue{Type: typector.Int64(), Value: str("1")}},
		{"int64", int64(-2), spanner.GenericColumnValue{Type: typector.Int64(), Value: str("-2")}},
		{"[]int", []int{1, 2}, spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_INT64), Value: listValue(str("1"), str("2"))}},
		{"NullInt64 invalid", spanner.NullInt64{}, spanner.GenericColumnValue{Type: typector.Int64(), Value: nullValue()}},

		{"bool", true, spanner.GenericColumnValue{Type: typector.Bool(), Value: structpb.NewBoolValue(true)}},
		{"float64", 1.5, spanner.GenericColumnValue{Type: typector.Float64(), Value: structpb.NewNumberValue(1.5)}},
		{"float64 NaN", math.NaN(), spanner.GenericColumnValue{Type: typector.Float64(), Value: str("NaN")}},
		{"float32", float32(1.5), spanner.GenericColumnValue{Type: typector.Float32(), Value: structpb.NewNumberValue(1.5)}},

		{"big.Rat value", *rat, spanner.GenericColumnValue{Type: typector.Numeric(), Value: str("99.500000000")}},
		{"*big.Rat nil", (*big.Rat)(nil), spanner.GenericColumnValue{Type: typector.Numeric(), Value: nullValue()}},
		{"NullNumeric valid", spanner.NullNumeric{Numeric: *rat, Valid: true}, spanner.GenericColumnValue{Type: typector.Numeric(), Value: str("99.500000000")}},
		{"PGNumeric", spanner.PGNumeric{Numeric: "99.5", Valid: true}, spanner.GenericColumnValue{Type: typector.PGNumeric(), Value: str("99.5")}},
		{"PGNumeric invalid", spanner.PGNumeric{}, spanner.GenericColumnValue{Type: typector.PGNumeric(), Value: nullValue()}},

		{"NullJSON valid", spanner.NullJSON{Value: map[string]any{"a": 1}, Valid: true}, spanner.GenericColumnValue{Type: typector.JSON(), Value: str(`{"a":1}`)}},
		{"NullJSON invalid", spanner.NullJSON{}, spanner.GenericColumnValue{Type: typector.JSON(), Value: nullValue()}},
		// A string Value marshals to a quoted JSON string, like the client;
		// pins the divergence from gcvctor.JSONFromNullable, which stores
		// string Values as wire JSON as-is.
		{"NullJSON string value", spanner.NullJSON{Value: `{"a":1}`, Valid: true}, spanner.GenericColumnValue{Type: typector.JSON(), Value: str(`"{\"a\":1}"`)}},
		{"PGJsonB valid", spanner.PGJsonB{Value: []any{1.0}, Valid: true}, spanner.GenericColumnValue{Type: typector.PGJSONB(), Value: str(`[1]`)}},
		{"PGJsonB string value", spanner.PGJsonB{Value: "x", Valid: true}, spanner.GenericColumnValue{Type: typector.PGJSONB(), Value: str(`"x"`)}},

		{"time.Time", ts, spanner.GenericColumnValue{Type: typector.Timestamp(), Value: str("2026-01-01T18:04:05.123456789Z")}},
		{"CommitTimestamp", spanner.CommitTimestamp, spanner.GenericColumnValue{Type: typector.Timestamp(), Value: str("spanner.commit_timestamp()")}},
		{"NullTime invalid", spanner.NullTime{}, spanner.GenericColumnValue{Type: typector.Timestamp(), Value: nullValue()}},
		{"*time.Time nil", (*time.Time)(nil), spanner.GenericColumnValue{Type: typector.Timestamp(), Value: nullValue()}},

		{"civil.Date", date, spanner.GenericColumnValue{Type: typector.Date(), Value: str("2026-01-02")}},
		{"uuid.UUID", u, spanner.GenericColumnValue{Type: typector.UUID(), Value: str("11111111-2222-3333-4444-555555555555")}},
		{"uuid.NullUUID invalid", uuid.NullUUID{}, spanner.GenericColumnValue{Type: typector.UUID(), Value: nullValue()}},
		{"Interval", iv, spanner.GenericColumnValue{Type: typector.Interval(), Value: str(iv.String())}},
		{"NullInterval invalid", spanner.NullInterval{}, spanner.GenericColumnValue{Type: typector.Interval(), Value: nullValue()}},

		{"custom string", myString("foo"), spanner.GenericColumnValue{Type: typector.String(), Value: str("foo")}},
		{"custom int64", myInt64(3), spanner.GenericColumnValue{Type: typector.Int64(), Value: str("3")}},
		{"custom slice of custom string", []myString{"a"}, spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: listValue(str("a"))}},
		{"named string slice", myTags{"a"}, spanner.GenericColumnValue{Type: typector.ElemCodeToArrayType(sppb.TypeCode_STRING), Value: listValue(str("a"))}},
		{"named bytes", myBlob("ab"), spanner.GenericColumnValue{Type: typector.Bytes(), Value: str(base64.StdEncoding.EncodeToString([]byte("ab")))}},
		{"named bytes nil", myBlob(nil), spanner.GenericColumnValue{Type: typector.Bytes(), Value: nullValue()}},
		{"named time struct", myTime(ts), spanner.GenericColumnValue{Type: typector.Timestamp(), Value: str("2026-01-01T18:04:05.123456789Z")}},
		{"named uuid array", myUUID(u), spanner.GenericColumnValue{Type: typector.UUID(), Value: str("11111111-2222-3333-4444-555555555555")}},

		{"encoder", myEncoder{v: "enc"}, spanner.GenericColumnValue{Type: typector.String(), Value: str("enc")}},

		{"proto message", dur, spanner.GenericColumnValue{Type: typector.FQNToProtoType("google.protobuf.Duration"), Value: str(base64.StdEncoding.EncodeToString(durBytes))}},
		{"proto message typed nil", (*durationpb.Duration)(nil), spanner.GenericColumnValue{Type: typector.FQNToProtoType("google.protobuf.Duration"), Value: nullValue()}},
		{"proto enum", sppb.TypeCode_INT64, spanner.GenericColumnValue{Type: typector.FQNToEnumType("google.spanner.v1.TypeCode"), Value: str("2")}},
		{"proto enum typed nil ptr", (*sppb.TypeCode)(nil), spanner.GenericColumnValue{Type: typector.FQNToEnumType("google.spanner.v1.TypeCode"), Value: nullValue()}},
		{"[]proto message nil", []*durationpb.Duration(nil), spanner.GenericColumnValue{Type: typector.ElemTypeToArrayType(typector.FQNToProtoType("google.protobuf.Duration")), Value: nullValue()}},
		{"[]proto enum", []sppb.TypeCode{sppb.TypeCode_BOOL}, spanner.GenericColumnValue{Type: typector.ElemTypeToArrayType(typector.FQNToEnumType("google.spanner.v1.TypeCode")), Value: listValue(str("1"))}},

		{"struct", tuple{ID: 1, Name: "n"}, spanner.GenericColumnValue{Type: tupleType, Value: listValue(str("1"), str("n"), str(""))}},
		{"nil struct pointer", (*tuple)(nil), spanner.GenericColumnValue{Type: tupleType, Value: nullValue()}},
		{"[]struct nil", []tuple(nil), spanner.GenericColumnValue{Type: typector.ElemTypeToArrayType(tupleType), Value: nullValue()}},
		{"[]*struct with nil", []*tuple{nil}, spanner.GenericColumnValue{Type: typector.ElemTypeToArrayType(tupleType), Value: listValue(nullValue())}},

		{"GCV passthrough", gcvctor.StringValue("x"), spanner.GenericColumnValue{Type: typector.String(), Value: str("x")}},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			got, err := spanenc.ValueOf(tt.input)
			if err != nil {
				t.Fatalf("ValueOf(%#v) error: %v", tt.input, err)
			}
			if diff := cmp.Diff(tt.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ValueOf(%#v) mismatch (-want +got):\n%s", tt.input, diff)
			}

			// The static type inference must agree with the encoded type
			// whenever it is defined for the dynamic Go type.
			wantType, err := spanenc.TypeFromGoType(reflect.TypeOf(tt.input))
			if errors.Is(err, spanenc.ErrTypeNotInferable) {
				return
			}
			if err != nil {
				t.Fatalf("TypeFromGoType(%T) error: %v", tt.input, err)
			}
			if diff := cmp.Diff(got.Type, wantType, protocmp.Transform()); diff != "" {
				t.Errorf("TypeFromGoType(%T) disagrees with ValueOf type (-value +static):\n%s", tt.input, diff)
			}
		})
	}
}

func TestValueOfErrors(t *testing.T) {
	t.Parallel()

	type withEmbeddedStruct struct {
		tuple
		Name string
	}

	hugeRat, ok := new(big.Rat).SetString("1e38")
	if !ok {
		t.Fatal("SetString failed")
	}

	for _, tt := range []struct {
		desc    string
		input   any
		wantErr error
	}{
		{"untyped nil", nil, spanenc.ErrUntypedNil},
		{"unsupported named int", myInt(1), spanenc.ErrUnsupportedType},
		{"unsupported map", map[string]int{}, spanenc.ErrUnsupportedType},
		{"[]GenericColumnValue", []spanner.GenericColumnValue{}, spanenc.ErrUnsupportedType},
		{"GCV with nil Type", spanner.GenericColumnValue{}, spanenc.ErrUnsupportedType},
		{"embedded struct field", withEmbeddedStruct{}, spanenc.ErrEmbeddedStructField},
		{"numeric out of range", hugeRat, spanenc.ErrNumericOutOfRange},
		{"NullProtoEnum invalid", spanner.NullProtoEnum{}, spanenc.ErrInvalidSource},
		{"NullProtoMessage invalid", spanner.NullProtoMessage{}, spanenc.ErrInvalidSource},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			_, err := spanenc.ValueOf(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValueOf(%#v) error = %v, want errors.Is(..., %v)", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestLossOfPrecisionHandling(t *testing.T) {
	t.Parallel()

	third := big.NewRat(1, 3) // not representable within scale 9

	t.Run("default validates", func(t *testing.T) {
		t.Parallel()
		if _, err := spanenc.ValueOf(third); !errors.Is(err, spanenc.ErrNumericOutOfRange) {
			t.Errorf("error = %v, want ErrNumericOutOfRange", err)
		}
	})
	t.Run("explicit NumericError validates", func(t *testing.T) {
		t.Parallel()
		_, err := spanenc.ValueOf(third, spanenc.WithLossOfPrecisionHandling(spanner.NumericError))
		if !errors.Is(err, spanenc.ErrNumericOutOfRange) {
			t.Errorf("error = %v, want ErrNumericOutOfRange", err)
		}
	})
	t.Run("NumericRound rounds", func(t *testing.T) {
		t.Parallel()
		got, err := spanenc.ValueOf(third, spanenc.WithLossOfPrecisionHandling(spanner.NumericRound))
		if err != nil {
			t.Fatal(err)
		}
		want := spanner.GenericColumnValue{Type: typector.Numeric(), Value: str("0.333333333")}
		if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("propagates into slices", func(t *testing.T) {
		t.Parallel()
		_, values, err := spanenc.ValuesFromSlice([]*big.Rat{third}, spanenc.WithLossOfPrecisionHandling(spanner.NumericRound))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff([]*structpb.Value{str("0.333333333")}, values, protocmp.Transform()); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("propagates into struct fields", func(t *testing.T) {
		t.Parallel()
		type row struct {
			N big.Rat
		}
		_, values, err := spanenc.StructColumnsAndValues(row{N: *third}, spanenc.WithLossOfPrecisionHandling(spanner.NumericRound))
		if err != nil {
			t.Fatal(err)
		}
		want := []spanner.GenericColumnValue{{Type: typector.Numeric(), Value: str("0.333333333")}}
		if diff := cmp.Diff(want, values, protocmp.Transform()); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestDecodeRoundTrip verifies wire compatibility with the real client
// library: values encoded by spanenc must decode back through
// spanner.GenericColumnValue.Decode.
func TestDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("string", func(t *testing.T) {
		t.Parallel()
		roundTrip(t, "foo")
	})
	t.Run("int64", func(t *testing.T) {
		t.Parallel()
		roundTrip(t, int64(42))
	})
	t.Run("bool", func(t *testing.T) {
		t.Parallel()
		roundTrip(t, true)
	})
	t.Run("float64 NaN wire string decodes", func(t *testing.T) {
		t.Parallel()
		got, err := spanenc.ValueOf(math.NaN())
		if err != nil {
			t.Fatal(err)
		}
		var dst float64
		if err := got.Decode(&dst); err != nil {
			t.Fatal(err)
		}
		if !math.IsNaN(dst) {
			t.Errorf("decoded %v, want NaN", dst)
		}
	})
	t.Run("[]string", func(t *testing.T) {
		t.Parallel()
		roundTrip(t, []string{"a", "b"})
	})
	t.Run("timestamp", func(t *testing.T) {
		t.Parallel()
		roundTrip(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	})
	t.Run("date", func(t *testing.T) {
		t.Parallel()
		roundTrip(t, civil.Date{Year: 2026, Month: 6, Day: 10})
	})
	t.Run("numeric", func(t *testing.T) {
		t.Parallel()
		got, err := spanenc.ValueOf(big.NewRat(199, 2))
		if err != nil {
			t.Fatal(err)
		}
		var dst big.Rat
		if err := got.Decode(&dst); err != nil {
			t.Fatal(err)
		}
		if dst.Cmp(big.NewRat(199, 2)) != 0 {
			t.Errorf("decoded %v, want 99.5", dst.String())
		}
	})
}

func roundTrip[T any](t *testing.T, in T) {
	t.Helper()
	got, err := spanenc.ValueOf(in)
	if err != nil {
		t.Fatalf("ValueOf(%#v) error: %v", in, err)
	}
	var dst T
	if err := got.Decode(&dst); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if diff := cmp.Diff(in, dst); diff != "" {
		t.Errorf("round trip mismatch (-in +decoded):\n%s", diff)
	}
}

func TestTypeFromGoTypeErrors(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		desc    string
		typ     reflect.Type
		wantErr error
	}{
		{"interface", reflect.TypeFor[any](), spanenc.ErrTypeNotInferable},
		{"GenericColumnValue", reflect.TypeFor[spanner.GenericColumnValue](), spanenc.ErrTypeNotInferable},
		{"Encoder implementation", reflect.TypeFor[myEncoder](), spanenc.ErrTypeNotInferable},
		{"NullProtoMessage", reflect.TypeFor[spanner.NullProtoMessage](), spanenc.ErrTypeNotInferable},
		{"named int", reflect.TypeFor[myInt](), spanenc.ErrUnsupportedType},
		{"nil type", nil, spanenc.ErrUntypedNil},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			_, err := spanenc.TypeFromGoType(tt.typ)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("TypeFromGoType(%v) error = %v, want errors.Is(..., %v)", tt.typ, err, tt.wantErr)
			}
		})
	}
}
