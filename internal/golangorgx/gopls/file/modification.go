// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package file

import "cuelang.org/go/internal/golangorgx/gopls/protocol"

// Modification represents a modification to a file.
type Modification struct {
	URI    protocol.DocumentURI
	Action Action

	// OnDisk is true if a watched file is changed on disk. If true,
	// Version will be -1 and Text will be nil.
	OnDisk bool

	// Version should not be used when the modification is an on-disk
	// modification.
	Version int32

	// ContentChanges will be nil when they are not supplied,
	// specifically on textDocument/didClose and for on-disk changes.
	ContentChanges []protocol.TextDocumentContentChangeEvent

	// LanguageID is only sent from the language client on
	// textDocument/didOpen.
	LanguageID string
}

// An Action is a type of file state change.
type Action int

const (
	UnknownAction = Action(iota)
	Open
	Change
	Close
	Save
	Create
	Delete
)

func (a Action) String() string {
	switch a {
	case Open:
		return "Open"
	case Change:
		return "Change"
	case Close:
		return "Close"
	case Save:
		return "Save"
	case Create:
		return "Create"
	case Delete:
		return "Delete"
	default:
		return "Unknown"
	}
}
