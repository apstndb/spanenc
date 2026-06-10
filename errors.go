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

import "errors"

var (
	// ErrUnsupportedType is returned when a Go value or type has no Cloud Spanner
	// client library encoding. It corresponds to the client's
	// "client doesn't support Go type" errors from encodeValue.
	ErrUnsupportedType = errors.New("spanenc: unsupported Go type")

	// ErrUntypedNil is returned by [ValueOf] for an untyped nil input.
	// This is a deliberate divergence from the client library, which encodes
	// untyped nil as a NULL value WITHOUT type information; spanvalue/gcvctor
	// consumers require a well-formed Type in every GenericColumnValue.
	// Use a typed nil (for example (*int64)(nil)) or
	// [github.com/apstndb/spanvalue/gcvctor.NullOf] instead.
	ErrUntypedNil = errors.New("spanenc: untyped nil value")

	// ErrNotStruct is returned by struct-shaped helpers ([StructColumns],
	// [RowTypeFor], [StructColumnsAndValues], and variants) when the input is
	// not a Go struct or pointer to struct. It mirrors the client's
	// errNotStruct from structToMutationParams.
	ErrNotStruct = errors.New("spanenc: not a Go struct type")

	// ErrNilStructPointer is returned by [StructColumnsAndValues] for a nil
	// pointer to struct. This is a deliberate divergence from the client's
	// structToMutationParams, which silently returns empty columns and values;
	// silently producing an empty row is a footgun for export use cases.
	ErrNilStructPointer = errors.New("spanenc: nil struct pointer")

	// ErrEmbeddedStructField is returned when a Go struct with embedded
	// (anonymous) fields is encoded as a Spanner STRUCT value. It mirrors the
	// client's errUnsupportedEmbeddedStructFields in encodeStruct. Note that
	// the row-shaped helpers ([StructColumns], [RowTypeFor],
	// [StructColumnsAndValues]) flatten embedded fields instead, mirroring the
	// client's mutation/ToStruct field listing.
	ErrEmbeddedStructField = errors.New("spanenc: embedded struct fields are not supported in STRUCT values")

	// ErrTypeNotInferable is returned by [TypeFor], [TypeFromGoType], and the
	// slice helpers when the Spanner type cannot be derived from the Go type
	// alone: types implementing [cloud.google.com/go/spanner.Encoder],
	// [cloud.google.com/go/spanner.GenericColumnValue],
	// [cloud.google.com/go/spanner.NullProtoMessage],
	// [cloud.google.com/go/spanner.NullProtoEnum], and interface types. The
	// client library never needs a type-only path because it always encodes
	// concrete values.
	ErrTypeNotInferable = errors.New("spanenc: Spanner type is not inferable from the Go type alone")

	// ErrInvalidSource is returned when a value cannot represent a typed NULL
	// and is invalid, mirroring the client's errNotValidSrc: NullProtoMessage
	// and NullProtoEnum with Valid == false.
	ErrInvalidSource = errors.New("spanenc: invalid (NULL) source value")

	// ErrNumericOutOfRange is returned when a NUMERIC input exceeds the
	// precision or scale supported by Cloud Spanner, mirroring the client's
	// validateNumeric under the default NumericError loss-of-precision
	// handling.
	ErrNumericOutOfRange = errors.New("spanenc: NUMERIC value exceeds supported precision or scale")
)
