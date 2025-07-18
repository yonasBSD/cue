// Copyright 2024 CUE Authors
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
	stdjson "encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/jsonschema/internal/externaltest"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetest"
)

// Pull in the external test data.
// The commit below references the JSON schema test main branch as of Sun May 19 19:01:03 2024 +0300

//go:generate go run vendor_external.go -- 9fc880bfb6d8ccd093bc82431f17d13681ffae8e

const testDir = "testdata/external"

// TestExternal runs the externally defined JSON Schema test suite,
// as defined in https://github.com/json-schema-org/JSON-Schema-Test-Suite.
func TestExternal(t *testing.T) {
	t.Parallel()
	tests, err := externaltest.ReadTestDir(testDir)
	qt.Assert(t, qt.IsNil(err))

	// Group the tests under a single subtest so that we can use
	// t.Parallel and still guarantee that all tests have completed
	// by the end.
	cuetdtest.SmallMatrix.Run(t, "tests", func(t *testing.T, m *cuetdtest.M) {
		t.Parallel()
		// Run tests in deterministic order so we get some consistency between runs.
		for _, filename := range slices.Sorted(maps.Keys(tests)) {
			schemas := tests[filename]
			t.Run(testName(filename), func(t *testing.T) {
				t.Parallel()
				for _, s := range schemas {
					t.Run(testName(s.Description), func(t *testing.T) {
						runExternalSchemaTests(t, m, filename, s)
					})
				}
			})
		}
	})
	if !cuetest.UpdateGoldenFiles {
		return
	}
	if t.Failed() {
		t.Fatalf("not writing test data back because of test failures (try CUE_UPDATE=force to proceed regardless of test regressions)")
	}
	err = externaltest.WriteTestDir(testDir, tests)
	qt.Assert(t, qt.IsNil(err))
	err = writeExternalTestStats(testDir, tests)
	qt.Assert(t, qt.IsNil(err))
}

var rxCharacterClassCategoryAlias = regexp.MustCompile(`\\p{(Cased_Letter|Close_Punctuation|Combining_Mark|Connector_Punctuation|Control|Currency_Symbol|Dash_Punctuation|Decimal_Number|Enclosing_Mark|Final_Punctuation|Format|Initial_Punctuation|Letter|Letter_Number|Line_Separator|Lowercase_Letter|Mark|Math_Symbol|Modifier_Letter|Modifier_Symbol|Nonspacing_Mark|Number|Open_Punctuation|Other|Other_Letter|Other_Number|Other_Punctuation|Other_Symbol|Paragraph_Separator|Private_Use|Punctuation|Separator|Space_Separator|Spacing_Mark|Surrogate|Symbol|Titlecase_Letter|Unassigned|Uppercase_Letter|cntrl|digit|punct)}`)

var supportsCharacterClassCategoryAlias = func() bool {
	//lint:ignore SA1000 this regular expression is meant to fail to compile on Go 1.24 and earlier
	_, err := regexp.Compile(`\p{Letter}`)
	return err == nil
}()

func runExternalSchemaTests(t *testing.T, m *cuetdtest.M, filename string, s *externaltest.Schema) {
	t.Logf("file %v", path.Join("testdata/external", filename))
	ctx := m.CueContext()
	jsonAST, err := json.Extract("schema.json", s.Schema)
	qt.Assert(t, qt.IsNil(err))
	jsonValue := ctx.BuildExpr(jsonAST)
	qt.Assert(t, qt.IsNil(jsonValue.Err()))
	versStr, _, _ := strings.Cut(strings.TrimPrefix(filename, "tests/"), "/")
	vers, ok := extVersionToVersion[versStr]
	if !ok {
		t.Fatalf("unknown JSON schema version for file %q", filename)
	}
	if vers == jsonschema.VersionUnknown {
		t.Skipf("skipping test for unknown schema version %v", versStr)
	}

	// The upcoming Go 1.25 implements Unicode category aliases in regular expressions,
	// such that e.g. \p{Letter} begins working on Go tip and 1.25 pre-releases.
	// Our tests must run on the latest two stable Go versions, currently 1.23 and 1.24,
	// where such character classes lead to schema compilation errors.
	//
	// As a temporary compromise, only run these tests on the broken and older Go versions.
	// With the testdata files being updated with the latest stable Go, 1.24,
	// this results in testdata reflecting what most Go users see with the latest Go,
	// while we are still able to use `go test` normally with Go tip.
	// TODO: swap around to expect the fixed behavior once Go 1.25.0 is released.
	// TODO: get rid of this whole thing once we require Go 1.25 or later in the future.
	if rxCharacterClassCategoryAlias.Match(s.Schema) && supportsCharacterClassCategoryAlias {
		t.Skip("regexp character classes for Unicode category aliases work only on Go 1.25 and later")
	}

	schemaAST, extractErr := jsonschema.Extract(jsonValue, &jsonschema.Config{
		StrictFeatures: true,
		DefaultVersion: vers,
	})
	var schemaValue cue.Value
	if extractErr == nil {
		// Round-trip via bytes because that's what will usually happen
		// to the generated schema.
		b, err := format.Node(schemaAST, format.Simplify())
		qt.Assert(t, qt.IsNil(err))
		t.Logf("extracted schema: %q", b)
		schemaValue = ctx.CompileBytes(b, cue.Filename("generated.cue"))
		if err := schemaValue.Err(); err != nil {
			extractErr = fmt.Errorf("cannot compile resulting schema: %v", errors.Details(err, nil))
		}
	}

	if extractErr != nil {
		t.Logf("location: %v", testdataPos(s))
		t.Logf("txtar:\n%s", schemaFailureTxtar(s))
		for _, test := range s.Tests {
			t.Run("", func(t *testing.T) {
				testFailed(t, m, &test.Skip, test, "could not compile schema")
			})
		}
		testFailed(t, m, &s.Skip, s, fmt.Sprintf("extract error: %v", extractErr))
		return
	}
	testSucceeded(t, m, &s.Skip, s)

	for _, test := range s.Tests {
		t.Run(testName(test.Description), func(t *testing.T) {
			defer func() {
				if t.Failed() || testing.Verbose() {
					t.Logf("txtar:\n%s", testCaseTxtar(s, test))
				}
			}()
			t.Logf("location: %v", testdataPos(test))
			instAST, err := json.Extract("instance.json", test.Data)
			if err != nil {
				t.Fatal(err)
			}

			qt.Assert(t, qt.IsNil(err), qt.Commentf("test data: %q; details: %v", test.Data, errors.Details(err, nil)))

			instValue := ctx.BuildExpr(instAST)
			qt.Assert(t, qt.IsNil(instValue.Err()))
			err = instValue.Unify(schemaValue).Validate(cue.Concrete(true))
			if test.Valid {
				if err != nil {
					testFailed(t, m, &test.Skip, test, errors.Details(err, nil))
				} else {
					testSucceeded(t, m, &test.Skip, test)
				}
			} else {
				if err == nil {
					testFailed(t, m, &test.Skip, test, "unexpected success")
				} else {
					testSucceeded(t, m, &test.Skip, test)
				}
			}
		})
	}
}

// testCaseTxtar returns a testscript that runs the given test.
func testCaseTxtar(s *externaltest.Schema, test *externaltest.Test) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "exec cue def json+jsonschema: schema.json\n")
	if !test.Valid {
		buf.WriteString("! ")
	}
	// TODO add $schema when one isn't already present?
	fmt.Fprintf(&buf, "exec cue vet -c instance.json json+jsonschema: schema.json\n")
	fmt.Fprintf(&buf, "\n")
	fmt.Fprintf(&buf, "-- schema.json --\n%s\n", indentJSON(s.Schema))
	fmt.Fprintf(&buf, "-- instance.json --\n%s\n", indentJSON(test.Data))
	return buf.String()
}

// testCaseTxtar returns a testscript that decodes the given schema.
func schemaFailureTxtar(s *externaltest.Schema) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "exec cue def -o schema.cue json+jsonschema: schema.json\n")
	fmt.Fprintf(&buf, "exec cat schema.cue\n")
	fmt.Fprintf(&buf, "exec cue vet schema.cue\n")
	fmt.Fprintf(&buf, "-- schema.json --\n%s\n", indentJSON(s.Schema))
	return buf.String()
}

func indentJSON(x stdjson.RawMessage) []byte {
	data, err := stdjson.MarshalIndent(x, "", "\t")
	if err != nil {
		panic(err)
	}
	return data
}

type positioner interface {
	Pos() token.Pos
}

// testName returns a test name that doesn't contain any
// slashes because slashes muck with matching.
func testName(s string) string {
	return strings.ReplaceAll(s, "/", "__")
}

// testFailed marks the current test as failed with the
// given error message, and updates the
// skip field pointed to by skipField if necessary.
func testFailed(t *testing.T, m *cuetdtest.M, skipField *externaltest.Skip, p positioner, errStr string) {
	if cuetest.UpdateGoldenFiles {
		if (*skipField)[m.Name()] == "" && !cuetest.ForceUpdateGoldenFiles {
			t.Fatalf("test regression; was succeeding, now failing: %v", errStr)
		}
		if *skipField == nil {
			*skipField = make(externaltest.Skip)
		}
		(*skipField)[m.Name()] = errStr
		return
	}
	if reason := (*skipField)[m.Name()]; reason != "" {
		qt.Assert(t, qt.Equals(reason, errStr), qt.Commentf("error message mismatch"))
		t.Skipf("skipping due to known error: %v", reason)
	}
	t.Fatal(errStr)
}

// testFails marks the current test as succeeded and updates the
// skip field pointed to by skipField if necessary.
func testSucceeded(t *testing.T, m *cuetdtest.M, skipField *externaltest.Skip, p positioner) {
	if cuetest.UpdateGoldenFiles {
		delete(*skipField, m.Name())
		if len(*skipField) == 0 {
			*skipField = nil
		}
		return
	}
	if reason := (*skipField)[m.Name()]; reason != "" {
		t.Fatalf("unexpectedly more correct behavior (test success) on skipped test")
	}
}

func testdataPos(p positioner) token.Position {
	pp := p.Pos().Position()
	pp.Filename = path.Join(testDir, pp.Filename)
	return pp
}

var extVersionToVersion = map[string]jsonschema.Version{
	"draft3":       jsonschema.VersionUnknown,
	"draft4":       jsonschema.VersionDraft4,
	"draft6":       jsonschema.VersionDraft6,
	"draft7":       jsonschema.VersionDraft7,
	"draft2019-09": jsonschema.VersionDraft2019_09,
	"draft2020-12": jsonschema.VersionDraft2020_12,
	"draft-next":   jsonschema.VersionUnknown,
}

func writeExternalTestStats(testDir string, tests map[string][]*externaltest.Schema) error {
	outf, err := os.Create("external_teststats.txt")
	if err != nil {
		return err
	}
	defer outf.Close()
	fmt.Fprintf(outf, "# Generated by CUE_UPDATE=1 go test. DO NOT EDIT\n")
	fmt.Fprintf(outf, "v2:\n")
	showStats(outf, "v2", false, tests)
	fmt.Fprintf(outf, "\n")
	fmt.Fprintf(outf, "v3:\n")
	showStats(outf, "v3", false, tests)
	fmt.Fprintf(outf, "\nOptional tests\n\n")
	fmt.Fprintf(outf, "v2:\n")
	showStats(outf, "v2", true, tests)
	fmt.Fprintf(outf, "\n")
	fmt.Fprintf(outf, "v3:\n")
	showStats(outf, "v3", true, tests)
	return nil
}

func showStats(outw io.Writer, version string, showOptional bool, tests map[string][]*externaltest.Schema) {
	schemaOK := 0
	schemaTot := 0
	testOK := 0
	testTot := 0
	schemaOKTestOK := 0
	schemaOKTestTot := 0
	for filename, schemas := range tests {
		isOptional := strings.Contains(filename, "/optional/")
		if isOptional != showOptional {
			continue
		}
		for _, schema := range schemas {
			schemaTot++
			if schema.Skip[version] == "" {
				schemaOK++
			}
			for _, test := range schema.Tests {
				testTot++
				if test.Skip[version] == "" {
					testOK++
				}
				if schema.Skip[version] == "" {
					schemaOKTestTot++
					if test.Skip[version] == "" {
						schemaOKTestOK++
					}
				}
			}
		}
	}
	fmt.Fprintf(outw, "\tschema extract (pass / total): %d / %d = %.1f%%\n", schemaOK, schemaTot, percent(schemaOK, schemaTot))
	fmt.Fprintf(outw, "\ttests (pass / total): %d / %d = %.1f%%\n", testOK, testTot, percent(testOK, testTot))
	fmt.Fprintf(outw, "\ttests on extracted schemas (pass / total): %d / %d = %.1f%%\n", schemaOKTestOK, schemaOKTestTot, percent(schemaOKTestOK, schemaOKTestTot))
}

func percent(a, b int) float64 {
	return (float64(a) / float64(b)) * 100.0
}
