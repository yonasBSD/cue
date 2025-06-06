// Copyright 2018 The CUE Authors
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

package json

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	cuejson "cuelang.org/go/encoding/json"
	"cuelang.org/go/internal/core/adt"
	internaljson "cuelang.org/go/internal/encoding/json"
	"cuelang.org/go/internal/pkg"
	"cuelang.org/go/internal/value"
)

// Compact generates the JSON-encoded src with insignificant space characters
// elided.
func Compact(src []byte) (string, error) {
	dst := bytes.Buffer{}
	if err := json.Compact(&dst, src); err != nil {
		return "", err
	}
	return dst.String(), nil
}

// Indent creates an indented form of the JSON-encoded src.
// Each element in a JSON object or array begins on a new,
// indented line beginning with prefix followed by one or more
// copies of indent according to the indentation nesting.
// The data appended to dst does not begin with the prefix nor
// any indentation, to make it easier to embed inside other formatted JSON data.
// Although leading space characters (space, tab, carriage return, newline)
// at the beginning of src are dropped, trailing space characters
// at the end of src are preserved and copied to dst.
// For example, if src has no trailing spaces, neither will dst;
// if src ends in a trailing newline, so will dst.
func Indent(src []byte, prefix, indent string) (string, error) {
	dst := bytes.Buffer{}
	if err := json.Indent(&dst, src, prefix, indent); err != nil {
		return "", err
	}
	return dst.String(), nil
}

// HTMLEscape returns the JSON-encoded src with <, >, &, U+2028 and
// U+2029 characters inside string literals changed to \u003c, \u003e, \u0026,
// \u2028, \u2029 so that the JSON will be safe to embed inside HTML <script>
// tags. For historical reasons, web browsers don't honor standard HTML escaping
// within <script> tags, so an alternative JSON encoding must be used.
func HTMLEscape(src []byte) string {
	dst := &bytes.Buffer{}
	json.HTMLEscape(dst, src)
	return dst.String()
}

// Marshal returns the JSON encoding of v.
func Marshal(v cue.Value) (string, error) {
	b, err := internaljson.Marshal(v)
	return string(b), err
}

// MarshalStream turns a list into a stream of JSON objects.
func MarshalStream(v cue.Value) (string, error) {
	// TODO: return an io.Reader and allow asynchronous processing.
	iter, err := v.List()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for iter.Next() {
		p, err := internaljson.Marshal(iter.Value())
		if err != nil {
			return "", err
		}
		b.Write(p)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// UnmarshalStream parses the JSON to a CUE instance.
func UnmarshalStream(data []byte) (ast.Expr, error) {
	d := cuejson.NewDecoder(nil, "", bytes.NewReader(data))

	a := []ast.Expr{}
	for {
		x, err := d.Extract()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		a = append(a, x)
	}

	return ast.NewList(a...), nil
}

// Unmarshal parses the JSON-encoded data.
func Unmarshal(b []byte) (ast.Expr, error) {
	if !json.Valid(b) {
		return nil, fmt.Errorf("json: invalid JSON")
	}
	expr, err := parser.ParseExpr("json", b)
	if err != nil {
		// NOTE: should never happen.
		return nil, errors.Wrapf(err, token.NoPos, "json: could not parse JSON")
	}
	return expr, nil
}

// Validate validates JSON and confirms it matches the constraints
// specified by v.
func Validate(b []byte, v pkg.Schema) (bool, error) {
	c := value.OpContext(v)
	return validate(c, b, v)
}

// validate is the actual implementation of Validate.
func validate(c *adt.OpContext, b []byte, v pkg.Schema) (bool, error) {
	if !json.Valid(b) {
		return false, fmt.Errorf("json: invalid JSON")
	}
	v2 := v.Context().CompileBytes(b, cue.Filename("json.Validate"))
	if err := v2.Err(); err != nil {
		return false, err
	}

	vx := adt.Unify(c, value.Vertex(v2), value.Vertex(v))
	v = value.Make(c, vx)
	if err := v.Err(); err != nil {
		return false, err
	}

	if err := v.Validate(cue.Final()); err != nil {
		return false, err
	}

	return true, nil
}
