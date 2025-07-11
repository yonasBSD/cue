// Copyright 2022 CUE Authors
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

package export_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/diff"
	"cuelang.org/go/internal/types"
	"golang.org/x/tools/txtar"
)

func TestSelfContained(t *testing.T) {
	t.Parallel()
	test := cuetxtar.TxTarTest{
		Name: "self",
		Root: "./testdata/selfcontained",

		Matrix: cuetdtest.FullMatrix,

		ToDo: map[string]string{
			"self-v3/selfcontained/import": `wa: reference "_hidden_567475F3" not found:`,

			"self/selfcontained/cyclic":            `reference not properly substituted`,
			"self-v3/selfcontained/cyclic":         `reference not properly substituted`,
			"self-v3-noshare/selfcontained/cyclic": `reference not properly substituted`,
		},
	}

	test.Run(t, func(t *cuetxtar.Test) {
		t.Parallel()
		r := t.CueContext()

		a := t.Instances()

		v := buildFile(t.T, r, a[0])

		p, ok := t.Value("path")
		if ok {
			v = v.LookupPath(cue.ParsePath(p))
		}

		var tValue types.Value
		v.Core(&tValue)

		self := *export.All
		self.SelfContained = true

		w := t.Writer("default")

		test := func() {
			file, errs := self.Def(tValue.R, "", tValue.V)

			errors.Print(w, errs, nil)
			_, _ = w.Write(formatNode(t.T, file))

			vf := patch(t.T, r, t.Archive, file)
			doDiff(t.T, v, vf)

			v = v.Unify(vf)
			doDiff(t.T, v, vf)
		}
		test()

		if _, ok := t.Value("inlineImports"); ok {
			w = t.Writer("expand_imports")
			self.InlineImports = true
			test()
		}
	})
}

func buildFile(t *testing.T, r *cue.Context, b *build.Instance) cue.Value {
	t.Helper()
	v := r.BuildInstance(b)
	if err := v.Err(); err != nil {
		t.Fatal(errors.Details(err, nil))
	}
	return v
}

// patch replaces the package at the root of the Archive with the given CUE
// file.
func patch(t *testing.T, r *cue.Context, orig *txtar.Archive, f *ast.File) cue.Value {
	a := *orig
	a.Files = slices.Clone(orig.Files)

	k := 0
	for _, f := range a.Files {
		if strings.HasSuffix(f.Name, ".cue") && !strings.ContainsRune(f.Name, '/') {
			continue
		}
		a.Files[k] = f
		k++
	}
	b, err := format.Node(f)
	if err != nil {
		t.Error(err)
	}

	a.Files = append(a.Files, txtar.File{
		Name: "in.cue",
		Data: b,
	})

	instance := cuetxtar.Load(&a, t.TempDir())[0]
	if instance.Err != nil {
		t.Fatal(instance.Err)
	}

	vn := buildFile(t, r, instance)
	if err := vn.Err(); err != nil {
		t.Fatal(err)
	}
	return vn
}

func doDiff(t *testing.T, v, w cue.Value) {
	var bb bytes.Buffer
	p := *diff.Schema
	p.SkipHidden = true
	d, script := p.Diff(v, w)
	if d != diff.Identity {
		diff.Print(&bb, script)
		t.Error(bb.String())
	}
}

// TestSC is for debugging purposes. Do not delete.
func TestSC(t *testing.T) {
	in := `
-- cue.mod/module.cue --
module: "example.com"
language: version: "v0.9.0"
-- in.cue --
	`
	if strings.HasSuffix(strings.TrimSpace(in), ".cue --") {
		t.Skip()
	}

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, t.TempDir())[0]
	if instance.Err != nil {
		t.Fatal(instance.Err)
	}

	version := cuecontext.EvalDefault
	version = cuecontext.EvalExperiment // Uncomment for eval V3
	r := cuecontext.New(cuecontext.EvaluatorVersion(version))
	(*runtime.Runtime)(r).SetDebugOptions(&cuedebug.Config{
		Sharing: true,
		LogEval: 1,
	})

	v := buildFile(t, r, instance)

	v = v.LookupPath(cue.ParsePath("a.b"))

	var tValue types.Value
	v.Core(&tValue)
	self := export.All
	self.SelfContained = true
	self.InlineImports = true

	file, errs := self.Def(tValue.R, "", tValue.V)
	if errs != nil {
		t.Fatal(errs)
	}

	b, _ := format.Node(file)
	t.Error(string(b))

	vf := patch(t, r, a, file)
	doDiff(t, v, vf)
}
