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

package cue

import (
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

// A Runtime is used for creating CUE Values.
//
// Any operation that involves two Values or Instances should originate from
// the same Runtime.
//
// The zero value of Runtime works for legacy reasons, but
// should not be used. It may panic at some point.
//
// Deprecated: use [Context].
type Runtime runtime.Runtime

func (r *Runtime) runtime() *runtime.Runtime {
	rt := (*runtime.Runtime)(r)
	rt.Init()
	return rt
}

type hiddenRuntime = Runtime

func (r *Runtime) complete(p *build.Instance, v *adt.Vertex) (*Instance, error) {
	idx := r.runtime()
	inst := getImportFromBuild(idx, p, v)
	inst.ImportPath = p.ImportPath
	if inst.Err != nil {
		return nil, inst.Err
	}
	return inst, nil
}

// Compile compiles the given source into an Instance. The source code may be
// provided as a string, byte slice, io.Reader. The name is used as the file
// name in position information. The source may import builtin packages. Use
// Build to allow importing non-builtin packages.
//
// Deprecated: use [Context] with methods like [Context.CompileString] or [Context.CompileBytes].
// The use of [Instance] is being phased out.
func (r *hiddenRuntime) Compile(filename string, source interface{}) (*Instance, error) {
	cfg := &runtime.Config{Filename: filename}
	v, p := r.runtime().Compile(cfg, source)
	return r.complete(p, v)
}

// Deprecated: use [Context.BuildInstances]. The use of [Instance] is being phased out.
func Build(instances []*build.Instance) []*Instance {
	if len(instances) == 0 {
		panic("cue: list of instances must not be empty")
	}
	var r Runtime
	a, _ := r.BuildInstances(instances)
	return a
}

// Deprecated: use [Context.BuildInstances]. The use of [Instance] is being phased out.
func (r *hiddenRuntime) BuildInstances(instances []*build.Instance) ([]*Instance, error) {
	index := r.runtime()

	loaded := []*Instance{}

	var errs errors.Error

	for _, p := range instances {
		v, _ := index.Build(nil, p)
		i := getImportFromBuild(index, p, v)
		errs = errors.Append(errs, i.Err)
		loaded = append(loaded, i)
	}

	// TODO: insert imports
	return loaded, errs
}
