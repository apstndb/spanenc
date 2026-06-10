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
	"github.com/apstndb/spanvalue/gcvctor"

	"cloud.google.com/go/spanner"
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// ValuesFromSlice converts a homogeneous slice into the shared element
// [sppb.Type] and one wire [structpb.Value] per element. Homogeneity is
// enforced through the static element type: T must have a type-inferable
// Spanner type (see [TypeFromGoType]), so interface element types — which
// could hold heterogeneous values — are rejected with [ErrTypeNotInferable]
// before any element is examined. Each encoded element is additionally
// verified against the inferred type.
//
// A nil slice returns the element type with a nil values slice. To express a
// typed NULL ARRAY versus an empty ARRAY at the GCV level, use
// [ArrayValueFromSlice].
func ValuesFromSlice[T any](vs []T) (*sppb.Type, []*structpb.Value, error) {
	elemType, gcvs, err := sliceElements(vs)
	if err != nil {
		return nil, nil, err
	}
	if vs == nil {
		return elemType, nil, nil
	}
	values := make([]*structpb.Value, len(gcvs))
	for i, e := range gcvs {
		values[i] = e.Value
	}
	return elemType, values, nil
}

// ArrayValueFromSlice converts a homogeneous slice into an ARRAY
// [spanner.GenericColumnValue] with the element type inferred statically
// from T (see [ValuesFromSlice] for the homogeneity rules). Following the
// client library's slice handling, a nil slice becomes a typed NULL ARRAY
// and an empty slice an empty ARRAY.
func ArrayValueFromSlice[T any](vs []T) (spanner.GenericColumnValue, error) {
	elemType, gcvs, err := sliceElements(vs)
	if err != nil {
		return gcv{}, err
	}
	if vs == nil {
		return gcvctor.NullArrayOf(elemType), nil
	}
	return gcvctor.ArrayValueOf(elemType, gcvs...)
}

// sliceElements infers the element type from T and encodes each element,
// verifying every element's encoded type against the inferred one.
func sliceElements[T any](vs []T) (*sppb.Type, []spanner.GenericColumnValue, error) {
	elemType, err := TypeFor[T]()
	if err != nil {
		return nil, nil, err
	}
	gcvs := make([]spanner.GenericColumnValue, len(vs))
	for i, v := range vs {
		e, err := ValueOf(v)
		if err != nil {
			return nil, nil, &gcvctor.ArrayElementError{Index: i, Err: err}
		}
		if !proto.Equal(elemType, e.Type) {
			return nil, nil, &gcvctor.ArrayElementError{Index: i, Err: gcvctor.ErrTypeMismatch}
		}
		gcvs[i] = e
	}
	return elemType, gcvs, nil
}
