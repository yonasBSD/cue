// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"fmt"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/event"
)

func (s *server) DidChangeWorkspaceFolders(ctx context.Context, params *protocol.DidChangeWorkspaceFoldersParams) error {
	// Per the comment in [server.Initialize], we only support a single
	// WorkspaceFolder for now. More precisely, the call to Initialize must have
	// a single WorkspaceFolder. Therefore a notification via
	// DidChangeWorkspaceFolders must not cause that folder to change (because
	// that is the invariant we are maintaining for now).
	//
	// So for now we simply error in case there is any DidChangeWorkspaceFolders
	// notification, rather than trying to be smart and work out "has the folder
	// change?". If this proves to be too simplistic or restrictive, then we can
	// revisit as part of removing this constraint.
	//
	// When we do add such support, we need to be how/if/where logic for
	// deduping views comes in.
	//
	// Ensure this logic is consistent with [server.Initialize].
	return fmt.Errorf("cue lsp only supports a single WorkspaceFolder for now")
}

// addView returns a Snapshot and a release function that must be called when
// the snapshot is no longer needed.
func (s *server) addView(ctx context.Context, name string, dir protocol.DocumentURI) (*cache.Snapshot, func(), error) {
	s.stateMu.Lock()
	state := s.state
	s.stateMu.Unlock()
	if state < serverInitialized {
		return nil, nil, fmt.Errorf("addView called before server initialized")
	}
	folder, err := s.newFolder(ctx, dir, name)
	if err != nil {
		return nil, nil, err
	}
	_, snapshot, release, err := s.session.NewView(ctx, folder)
	return snapshot, release, err
}

func (s *server) DidChangeConfiguration(ctx context.Context, _ *protocol.DidChangeConfigurationParams) error {
	ctx, done := event.Start(ctx, "lsp.Server.didChangeConfiguration")
	defer done()

	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()
	if s.Options().VerboseWorkDoneProgress {
		work := s.progress.Start(ctx, DiagnosticWorkTitle(FromDidChangeConfiguration), "Calculating diagnostics...", nil, nil)
		go func() {
			wg.Wait()
			work.End(ctx, "Done.")
		}()
	}

	// Apply any changes to the session-level settings.
	options, err := s.fetchFolderOptions(ctx, "")
	if err != nil {
		return err
	}
	s.SetOptions(options)

	// Collect options for all workspace folders.
	seen := make(map[protocol.DocumentURI]bool)
	var newFolders []*cache.Folder
	for _, view := range s.session.Views() {
		folder := view.Folder()
		if seen[folder.Dir] {
			continue
		}
		seen[folder.Dir] = true
		newFolder, err := s.newFolder(ctx, folder.Dir, folder.Name)
		if err != nil {
			return err
		}
		newFolders = append(newFolders, newFolder)
	}
	s.session.UpdateFolders(ctx, newFolders)

	// The view set may have been updated above.
	viewsToDiagnose := make(map[*cache.View][]protocol.DocumentURI)
	for _, view := range s.session.Views() {
		viewsToDiagnose[view] = nil
	}

	modCtx, modID := s.needsDiagnosis(ctx, viewsToDiagnose)
	wg.Add(1)
	go func() {
		s.diagnoseChangedViews(modCtx, modID, viewsToDiagnose, FromDidChangeConfiguration)
		wg.Done()
	}()

	return nil
}
