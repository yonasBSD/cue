// Copyright 2019 CUE Authors
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

package jsonschema_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
	_ "cuelang.org/go/pkg"
)

// TestDecode reads the testdata/*.txtar files, converts the contained
// JSON schema to CUE and compares it against the output.
//
// Set CUE_UPDATE=1 to update test files with the corresponding output.
//
// Each test extracts the JSON Schema from a schema file (either
// schema.json or schema.yaml) and writes the result to
// out/decode/extract.
//
// If there are any files in the "test" directory in the txtar, each one
// is extracted and validated against the extracted schema. If the file
// name starts with "err-" it is expected to fail, otherwise it is
// expected to succeed.
//
// If the first line of a test file starts with a "#" character,
// it should start with `#schema` followed by a CUE path
// of the schema to test within the extracted schema.
//
// The #noverify tag in the txtar header causes verification and
// instance tests to be skipped.
//
// The #version: <version> tag selects the default schema version URI to use.
// As a special case, when this is "openapi", OpenAPI extraction
// mode is enabled.
func TestDecode(t *testing.T) {
	t.Parallel()
	test := cuetxtar.TxTarTest{
		Root:   "./testdata/txtar",
		Name:   "decode",
		Matrix: cuetdtest.FullMatrix,
	}
	test.Run(t, func(t *cuetxtar.Test) {
		t.Parallel()
		ctx := t.CueContext()

		fsys, err := txtar.FS(t.Archive)
		if err != nil {
			t.Fatal(err)
		}
		v, err := readSchema(ctx, fsys)
		if err != nil {
			t.Fatal(err)
		}

		cfg := &jsonschema.Config{}

		if t.HasTag("brokenInV2") && t.M.Name() == "v2" {
			t.Skip("skipping because test is broken under the v2 evaluator")
		}

		if versStr, ok := t.Value("version"); ok {
			// TODO most schemas have neither an explicit $schema or a #version
			// tag, so when we update the default version, they could break.
			// We should probably change most of the tests to use an explicit $schema
			// field apart from when we're explicitly testing the default version logic.
			switch versStr {
			case "openapi":
				cfg.DefaultVersion = jsonschema.VersionOpenAPI
				cfg.Map = func(p token.Pos, a []string) ([]ast.Label, error) {
					// Just for testing: does not validate the path.
					return []ast.Label{ast.NewIdent("#" + a[len(a)-1])}, nil
				}
				cfg.Root = "#/components/schemas/"
				cfg.StrictKeywords = true // encoding/openapi always uses strict keywords
			case "k8sAPI":
				cfg.DefaultVersion = jsonschema.VersionKubernetesAPI
				cfg.Root = "#/components/schemas/"
				cfg.StrictKeywords = true
			case "k8sCRD":
				if v.IncompleteKind() == cue.ListKind {
					t.Skip("regular Extract does not cope with extracting CRDs from arrays")
				}
				cfg.DefaultVersion = jsonschema.VersionKubernetesCRD
				// Default to the first version; can be overridden with #root.
				cfg.Root = "#/spec/versions/0/schema/openAPIV3Schema"
				cfg.StrictKeywords = true // CRDs always use strict keywords
				cfg.SingleRoot = true
			default:
				vers, err := jsonschema.ParseVersion(versStr)
				qt.Assert(t, qt.IsNil(err))
				cfg.DefaultVersion = vers
			}
		}
		if root, ok := t.Value("root"); ok {
			cfg.Root = root
		}
		cfg.Strict = t.HasTag("strict")
		cfg.StrictKeywords = cfg.StrictKeywords || t.HasTag("strictKeywords")
		cfg.AllowNonExistentRoot = t.HasTag("allowNonExistentRoot")
		cfg.StrictFeatures = t.HasTag("strictFeatures")
		if t.HasTag("singleRoot") {
			cfg.SingleRoot = true
		}
		cfg.PkgName, _ = t.Value("pkgName")

		w := t.Writer("extract")
		expr, err := jsonschema.Extract(v, cfg)
		if err != nil {
			got := "ERROR:\n" + errors.Details(err, nil)
			w.Write([]byte(got))
			return
		}
		if expr == nil {
			t.Fatal("no expression was extracted")
		}

		b, err := format.Node(expr, format.Simplify())
		if err != nil {
			t.Fatal(errors.Details(err, nil))
		}
		b = append(bytes.TrimSpace(b), '\n')
		w.Write(b)
		if t.HasTag("noverify") {
			return
		}
		// Verify that the generated CUE compiles.
		schemav := ctx.CompileBytes(b, cue.Filename("generated.cue"))
		if err := schemav.Err(); err != nil {
			t.Fatal(errors.Details(err, nil), qt.Commentf("generated code: %q", b))
		}
		testEntries, err := fs.ReadDir(fsys, "test")
		if err != nil {
			return
		}
		for _, e := range testEntries {
			file := path.Join("test", e.Name())
			var v cue.Value
			base := ""
			testData, err := fs.ReadFile(fsys, file)
			if err != nil {
				t.Fatal(err)
			}
			var schemaPath cue.Path
			if bytes.HasPrefix(testData, []byte("#")) {
				directiveBytes, rest, _ := bytes.Cut(testData, []byte("\n"))
				// Replace the directive with a newline so the line numbers
				// are correct in any error messages.
				testData = append([]byte("\n"), rest...)
				directive := string(directiveBytes)
				verb, arg, ok := strings.Cut(directive, " ")
				if verb != "#schema" {
					t.Fatalf("unknown directive %q in test file %v", directiveBytes, file)
				}
				if !ok {
					t.Fatalf("no schema path argument to #schema directive in %s", file)
				}
				schemaPath = cue.ParsePath(arg)
				qt.Assert(t, qt.IsNil(schemaPath.Err()))
			}

			switch {
			case strings.HasSuffix(file, ".json"):
				expr, err := json.Extract(file, testData)
				if err != nil {
					t.Fatal(err)
				}
				v = ctx.BuildExpr(expr)
				base = strings.TrimSuffix(e.Name(), ".json")

			case strings.HasSuffix(file, ".yaml"):
				file, err := yaml.Extract(file, testData)
				if err != nil {
					t.Fatal(err)
				}
				v = ctx.BuildFile(file)
				base = strings.TrimSuffix(e.Name(), ".yaml")
			default:
				t.Fatalf("unknown file encoding for test file %v", file)
			}
			if err := v.Err(); err != nil {
				t.Fatalf("error building expression for test %v: %v", file, err)
			}
			subSchema := schemav.LookupPath(schemaPath)
			if !subSchema.Exists() {
				t.Fatalf("path %q does not exist within schema", schemaPath)
			}
			rv := subSchema.Unify(v)
			if strings.HasPrefix(e.Name(), "err-") {
				err := rv.Err()
				if err == nil {
					t.Fatalf("test %v unexpectedly passes", file)
				}
				if t.M.IsDefault() {
					// The error results of the different evaluators can vary,
					// so only test the exact results for the default evaluator.
					t.Writer(path.Join("testerr", base)).Write([]byte(errors.Details(err, nil)))
				}
			} else {
				if err := rv.Err(); err != nil {
					t.Fatalf("test %v unexpectedly fails: %v", file, errors.Details(err, nil))
				}
			}
		}
	})
}

func TestDecodeCRD(t *testing.T) {
	t.Parallel()
	test := cuetxtar.TxTarTest{
		Root:   "./testdata/txtar",
		Name:   "decodeCRD",
		Matrix: cuetdtest.FullMatrix,
	}
	test.Run(t, func(t *cuetxtar.Test) {
		if versStr, ok := t.Value("version"); !ok || versStr != "k8sCRD" {
			t.Skip("test not relevant to CRDs")
		}
		t.Parallel()

		ctx := t.CueContext()

		fsys, err := txtar.FS(t.Archive)
		if err != nil {
			t.Fatal(err)
		}
		v, err := readSchema(ctx, fsys)
		if err != nil {
			t.Fatal(err)
		}
		crds, err := jsonschema.ExtractCRDs(v, &jsonschema.CRDConfig{})
		if err != nil {
			w := t.Writer("extractCRD/error")
			fmt.Fprintf(w, "%v\n", err)
			return
		}
		for i, crd := range crds {
			for _, version := range slices.Sorted(maps.Keys(crd.Versions)) {
				w := t.Writer(fmt.Sprintf("extractCRD/%d/%s", i, version))
				f := crd.Versions[version]
				b, err := format.Node(f, format.Simplify())
				if err != nil {
					t.Fatal(errors.Details(err, nil))
				}
				b = append(bytes.TrimSpace(b), '\n')
				w.Write(b)
				// TODO test that schema actually works.
			}
		}
	})
}

func readSchema(ctx *cue.Context, fsys fs.FS) (cue.Value, error) {
	jsonData, jsonErr := fs.ReadFile(fsys, "schema.json")
	yamlData, yamlErr := fs.ReadFile(fsys, "schema.yaml")
	var v cue.Value
	switch {
	case jsonErr == nil && yamlErr == nil:
		return cue.Value{}, fmt.Errorf("cannot define both schema.json and schema.yaml")
	case jsonErr == nil:
		expr, err := json.Extract("schema.json", jsonData)
		if err != nil {
			return cue.Value{}, err
		}
		v = ctx.BuildExpr(expr)
	case yamlErr == nil:
		file, err := yaml.Extract("schema.yaml", yamlData)
		if err != nil {
			return cue.Value{}, err
		}
		v = ctx.BuildFile(file)
	default:
		return cue.Value{}, fmt.Errorf("no schema.yaml or schema.json file found for test")
	}
	if err := v.Err(); err != nil {
		return cue.Value{}, err
	}
	return v, nil
}

func TestMapURL(t *testing.T) {
	t.Parallel()
	v := cuecontext.New().CompileString(`
type: "object"
properties: x: $ref: "https://something.test/foo#/definitions/blah"
`)
	var calls []string
	expr, err := jsonschema.Extract(v, &jsonschema.Config{
		MapURL: func(u *url.URL) (string, cue.Path, error) {
			calls = append(calls, u.String())
			return "other.test/something:blah", cue.ParsePath("#Foo.bar"), nil
		},
	})
	qt.Assert(t, qt.IsNil(err))
	b, err := format.Node(expr, format.Simplify())
	if err != nil {
		t.Fatal(errors.Details(err, nil))
	}
	qt.Assert(t, qt.DeepEquals(calls, []string{"https://something.test/foo"}))
	qt.Assert(t, qt.Equals(string(b), `
import "other.test/something:blah"

x?: blah.#Foo.bar.#blah
...
`[1:]))
}

func TestMapURLErrors(t *testing.T) {
	v := cuecontext.New().CompileString(`
type: "object"
properties: {
	x: $ref: "https://something.test/foo#/definitions/x"
	y: $ref: "https://something.test/foo#/definitions/y"
}
`, cue.Filename("foo.cue"))
	_, err := jsonschema.Extract(v, &jsonschema.Config{
		MapURL: func(u *url.URL) (string, cue.Path, error) {
			return "", cue.Path{}, fmt.Errorf("some error")
		},
	})
	qt.Assert(t, qt.Equals(errors.Details(err, nil), `
cannot determine CUE location for JSON Schema location id=https://something.test/foo#/definitions/x: some error:
    foo.cue:4:5
cannot determine CUE location for JSON Schema location id=https://something.test/foo#/definitions/y: some error:
    foo.cue:5:5
`[1:]))
}

func TestMapRef(t *testing.T) {
	t.Parallel()
	v := cuecontext.New().CompileString(`
type: "object"
$id: "https://this.test"
$defs: foo: type: "string"
properties: x: $ref: "https://something.test/foo#/$defs/blah"
`)
	var calls []string
	expr, err := jsonschema.Extract(v, &jsonschema.Config{
		MapRef: func(loc jsonschema.SchemaLoc) (string, cue.Path, error) {
			calls = append(calls, loc.String())
			switch loc.ID.String() {
			case "https://this.test#/$defs/foo":
				return "", cue.ParsePath("#x.#def.#foo"), nil
			case "https://something.test/foo#/$defs/blah":
				return "other.test/something:blah", cue.ParsePath("#Foo.bar"), nil
			case "https://this.test":
				return "", cue.Path{}, nil
			}
			t.Errorf("unexpected ID")
			return "", cue.Path{}, fmt.Errorf("unexpected ID %q passed to MapRef", loc.ID)
		},
	})
	qt.Assert(t, qt.IsNil(err))
	b, err := format.Node(expr, format.Simplify())
	if err != nil {
		t.Fatal(errors.Details(err, nil))
	}
	qt.Assert(t, qt.DeepEquals(calls, []string{
		"id=https://this.test#/$defs/foo localPath=$defs.foo",
		"id=https://something.test/foo#/$defs/blah",
		"id=https://this.test localPath=",
		"id=https://something.test/foo#/$defs/blah",
	}))
	qt.Assert(t, qt.Equals(string(b), `
import "other.test/something:blah"

@jsonschema(id="https://this.test")
x?: blah.#Foo.bar

#x: #def: #foo: string
...
`[1:]))
}

func TestMapRefExternalRefForInternalSchema(t *testing.T) {
	t.Parallel()
	v := cuecontext.New().CompileString(`
type: "object"
$id: "https://this.test"
$defs: foo: {
	description: "foo can be a number or a string"
	type: ["number", "string"]
}
$defs: bar: type: "boolean"
$ref: "#/$defs/foo"
`)
	var calls []string
	defines := make(map[string]string)
	expr, err := jsonschema.Extract(v, &jsonschema.Config{
		MapRef: func(loc jsonschema.SchemaLoc) (string, cue.Path, error) {
			calls = append(calls, loc.String())
			switch loc.ID.String() {
			case "https://this.test#/$defs/foo":
				return "otherpkg.example/foo", cue.ParsePath("#x"), nil
			case "https://this.test#/$defs/bar":
				return "otherpkg.example/bar", cue.ParsePath("#x"), nil
			case "https://this.test":
				return "", cue.Path{}, nil
			}
			t.Errorf("unexpected ID")
			return "", cue.Path{}, fmt.Errorf("unexpected ID %q passed to MapRef", loc.ID)
		},
		DefineSchema: func(importPath string, path cue.Path, e ast.Expr, c *ast.CommentGroup) {
			if c != nil {
				ast.AddComment(e, c)
			}
			data, err := format.Node(e)
			if err != nil {
				t.Errorf("cannot format: %v", err)
				return
			}
			defines[fmt.Sprintf("%s.%v", importPath, path)] = string(data)
		},
	})
	qt.Assert(t, qt.IsNil(err))
	b, err := format.Node(expr, format.Simplify())
	if err != nil {
		t.Fatal(errors.Details(err, nil))
	}
	qt.Check(t, qt.DeepEquals(calls, []string{
		"id=https://this.test#/$defs/foo localPath=$defs.foo",
		"id=https://this.test#/$defs/bar localPath=$defs.bar",
		"id=https://this.test localPath=",
	}))
	qt.Check(t, qt.Equals(string(b), `
import "otherpkg.example/foo"

@jsonschema(id="https://this.test")
foo.#x & {
	...
}
`[1:]))
	qt.Check(t, qt.DeepEquals(defines, map[string]string{
		"otherpkg.example/bar.#x": "bool",
		"otherpkg.example/foo.#x": "// foo can be a number or a string\nnumber | string",
	}))
}
