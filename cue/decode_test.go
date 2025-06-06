// Copyright 2021 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cue_test

import (
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/cuetdtest"
	"github.com/cockroachdb/apd/v3"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
)

func TestDecode(t *testing.T) {
	type Nested struct {
		P *int `json:"P"`
	}
	type fields struct {
		A int `json:"A"`
		B int `json:"B"`
		C int `json:"C"`
		M map[string]interface{}
		*Nested
	}
	one := 1
	intList := func(ints ...int) *[]int {
		ints = append([]int{}, ints...)
		return &ints
	}
	testCases := []struct {
		value string
		dst   interface{}
		want  interface{}
		err   string
	}{{
		// clear pointer
		value: `null`,
		dst:   &[]int{1},
		want:  []int(nil),
	}, {

		value: `1`,
		err:   "cannot decode into unsettable value",
	}, {
		dst:   new(interface{}),
		value: `_|_`,
		err:   "explicit error (_|_ literal) in source",
	}, {
		// clear pointer
		value: `null`,
		dst:   &[]int{1},
		want:  []int(nil),
	}, {
		// clear pointer
		value: `[null]`,
		dst:   &[]*int{&one},
		want:  []*int{nil},
	}, {
		value: `true`,
		dst:   new(bool),
		want:  true,
	}, {
		value: `false`,
		dst:   new(bool),
		want:  false,
	}, {
		value: `bool`,
		dst:   new(bool),
		err:   "cannot convert non-concrete value bool",
	}, {
		value: `_`,
		dst:   new([]int),
		want:  []int(nil),
	}, {
		value: `"str"`,
		dst:   new(string),
		want:  "str",
	}, {
		value: `"str"`,
		dst:   new(int),
		err:   "cannot use value \"str\" (type string) as int",
	}, {
		value: `'bytes'`,
		dst:   new([]byte),
		want:  []byte("bytes"),
	}, {
		value: `'bytes'`,
		dst:   &[3]byte{},
		want:  [3]byte{0x62, 0x79, 0x74},
	}, {
		value: `1`,
		dst:   new(float32),
		want:  float32(1),
	}, {
		value: `500`,
		dst:   new(uint8),
		err:   "integer 500 overflows uint8",
	}, {
		value: `501`,
		dst:   new(int8),
		err:   "integer 501 overflows int8",
	}, {
		value: `{}`,
		dst:   &fields{},
		want:  fields{},
	}, {
		value: `{A:1,b:2,c:3}`,
		dst:   &fields{},
		want:  fields{A: 1, B: 2, C: 3},
	}, {
		// allocate map
		value: `{a:1,m:{a: 3}}`,
		dst:   &fields{},
		want: fields{A: 1,
			M: map[string]interface{}{"a": int64(3)}},
	}, {
		// indirect int
		value: `{p: 1}`,
		dst:   &fields{},
		want:  fields{Nested: &Nested{P: &one}},
	}, {
		value: `{for k, v in y if v > 1 {"\(k)": v} }
		y: {a:1,b:2,c:3}`,
		dst:  &fields{},
		want: fields{B: 2, C: 3},
	}, {
		value: `{a:1,b:2,c:int}`,
		dst:   new(fields),
		err:   "c: cannot convert non-concrete value int",
	}, {
		value: `[]`,
		dst:   intList(),
		want:  *intList(),
	}, {
		value: `[1,2,3]`,
		dst:   intList(),
		want:  *intList(1, 2, 3),
	}, {
		// shorten list
		value: `[1,2,3]`,
		dst:   intList(1, 2, 3, 4),
		want:  *intList(1, 2, 3),
	}, {
		// shorter array
		value: `[1,2,3]`,
		dst:   &[2]int{},
		want:  [2]int{1, 2},
	}, {
		// longer array
		value: `[1,2,3]`,
		dst:   &[4]int{},
		want:  [4]int{1, 2, 3, 0},
	}, {
		value: `[for x in #y if x > 1 { x }]
				#y: [1,2,3]`,
		dst:  intList(),
		want: *intList(2, 3),
	}, {
		value: `[int]`,
		dst:   intList(),
		err:   "0: cannot convert non-concrete value int",
	}, {
		value: `{a: 1, b: 2, c: 3}`,
		dst:   &map[string]int{},
		want:  map[string]int{"a": 1, "b": 2, "c": 3},
	}, {
		value: `{"1": 1, "-2": 2, "3": 3}`,
		dst:   &map[int]int{},
		want:  map[int]int{1: 1, -2: 2, 3: 3},
	}, {
		value: `{"1": 1, "2": 2, "3": 3}`,
		dst:   &map[uint]int{},
		want:  map[uint]int{1: 1, 2: 2, 3: 3},
	}, {
		value: `{a: 1, b: 2, c: true, d: e: 2}`,
		dst:   &map[string]interface{}{},
		want: map[string]interface{}{
			"a": int64(1), "b": int64(2), "c": true,
			"d": map[string]interface{}{"e": int64(2)}},
	}, {
		value: `{a: b: *2 | int}`,
		dst:   &map[string]interface{}{},
		want:  map[string]interface{}{"a": map[string]interface{}{"b": int64(2)}},
	}, {
		value: `{a: 1, b: 2, c: true}`,
		dst:   &map[string]int{},
		err:   "c: cannot use value true (type bool) as int",
	}, {
		value: `{"300": 3}`,
		dst:   &map[int8]int{},
		err:   "key integer 300 overflows int8",
	}, {
		value: `{"300": 3}`,
		dst:   &map[uint8]int{},
		err:   "key integer 300 overflows uint8",
	}, {
		// Issue #1401
		value: `a: b: _ | *[0, ...]`,
		dst:   &map[string]interface{}{},
		want: map[string]interface{}{
			"a": map[string]interface{}{
				"b": []interface{}{int64(0)},
			},
		},
	}, {
		// Issue #1466
		value: `{"x": "1s"}
		`,
		dst:  &S{},
		want: S{X: Duration{D: 1000000000}},
	}, {
		// Issue #1466
		value: `{"x": '1s'}
			`,
		dst:  &S{},
		want: S{X: Duration{D: 1000000000}},
	}, {
		// Issue #1466
		value: `{"x": 1}
				`,
		dst: &S{},
		err: "Decode: x: cannot use value 1 (type int) as (string|bytes)",
	}, {
		value: `[]`,
		dst:   new(interface{}),
		want:  []interface{}{},
	}, {
		// large integer which doesn't fit into an int32 or int on 32-bit platforms
		value: `8000000000`,
		dst:   new(interface{}),
		want:  int64(8000000000),
	}, {
		// same as the above, but negative
		value: `-8000000000`,
		dst:   new(interface{}),
		want:  int64(-8000000000),
	}, {
		// even larger integer which doesn't fit into an int64
		value: `9500000000000000000`,
		dst:   new(interface{}),
		want:  bigInt("9500000000000000000"),
	}, {
		// same as the above, but negative
		value: `-9500000000000000000`,
		dst:   new(interface{}),
		want:  bigInt("-9500000000000000000"),
	}, {
		value: `9500000000000000000`,
		dst:   new(*big.Int),
		want:  bigInt("9500000000000000000"),
	}, {
		value: `-9500000000000000000`,
		dst:   new(*big.Int),
		want:  bigInt("-9500000000000000000"),
	}, {
		// TODO: should this work?
		value: `9500000000000000000`,
		dst:   new(*apd.Decimal),
		err:   "Decode: cannot use value 9500000000000000000 (type int) as (string|bytes)",
	}, {
		// TODO: should this work?
		value: `-9500000000000000000`,
		dst:   new(*apd.Decimal),
		err:   "Decode: cannot use value -9500000000000000000 (type int) as (string|bytes)",
	}, {
		// large float which doesn't fit into a float32
		value: `1.797693134e+308`,
		dst:   new(interface{}),
		want:  float64(1.797693134e+308),
	}, {
		value: `1.99769313499e+508`,
		dst:   new(interface{}),
		want:  bigFloat(`1.99769313499e+508`),
	}, {
		value: `-1.99769313499e+508`,
		dst:   new(interface{}),
		want:  bigFloat(`-1.99769313499e+508`),
	}, {
		value: `1.99769313499e+508`,
		dst:   new(*big.Float),
		want:  bigFloat(`1.99769313499e+508`),
	}, {
		value: `-1.99769313499e+508`,
		dst:   new(*big.Float),
		want:  bigFloat(`-1.99769313499e+508`),
	}}
	for _, tc := range testCases {
		cuetdtest.FullMatrix.Run(t, tc.value, func(t *testing.T, m *cuetdtest.M) {
			err := getValue(m, tc.value).Decode(tc.dst)
			checkFatal(t, err, tc.err, "init")

			got := reflect.ValueOf(tc.dst).Elem().Interface()
			if diff := cmp.Diff(got, tc.want, cmp.Comparer(func(a, b *big.Int) bool {
				return a.Cmp(b) == 0
			}), cmp.Comparer(func(a, b *big.Float) bool {
				return a.Cmp(b) == 0
			})); diff != "" {
				t.Error(diff)
				t.Errorf("\n%#v\n%#v", got, tc.want)
			}
		})
	}
}

func bigInt(s string) *big.Int {
	n, _ := big.NewInt(0).SetString(s, 10)
	return n
}

func bigFloat(s string) *big.Float {
	f := &big.Float{}
	f, _, _ = f.Parse(s, 0)
	return f
}

func TestDecodeIntoCUEValue(t *testing.T) {
	cuetdtest.FullMatrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		// We should be able to decode into a CUE value so we can
		// decode partially incomplete values into Go.
		// This test doesn't fit within the table used by TestDecode
		// because cue values aren't easily comparable with cmp.Diff.
		var st struct {
			X cue.Value `json:"x"`
		}
		err := getValue(m, `x: string`).Decode(&st)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fmt.Sprint(st.X), "string"))

		// Check we can decode into a top level value.
		var v cue.Value
		err = getValue(m, `int`).Decode(&v)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fmt.Sprint(v), "int"))
	})
}

type Duration struct {
	D time.Duration
}
type S struct {
	X Duration `json:"x"`
}

func (d *Duration) UnmarshalText(data []byte) error {
	v, err := time.ParseDuration(string(data))
	if err != nil {
		return err
	}
	d.D = v
	return nil
}

func (d *Duration) MarshalText() ([]byte, error) {
	return []byte(d.D.String()), nil
}
