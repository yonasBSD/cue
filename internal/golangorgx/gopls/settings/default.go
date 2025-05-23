// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package settings

import (
	"sync"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/protocol/command"
)

var (
	optionsOnce    sync.Once
	defaultOptions *Options
)

// DefaultOptions is the options that are used for Gopls execution independent
// of any externally provided configuration (LSP initialization, command
// invocation, etc.).
func DefaultOptions(overrides ...func(*Options)) *Options {
	optionsOnce.Do(func() {
		var commands []string
		for _, c := range command.Commands {
			commands = append(commands, c.ID())
		}
		defaultOptions = &Options{
			ClientOptions: ClientOptions{
				InsertTextFormat:                           protocol.PlainTextTextFormat,
				PreferredContentFormat:                     protocol.Markdown,
				ConfigurationSupported:                     true,
				DynamicConfigurationSupported:              true,
				DynamicRegistrationSemanticTokensSupported: true,
				DynamicWatchedFilesSupported:               true,
				LineFoldingOnly:                            false,
				HierarchicalDocumentSymbolSupport:          true,
			},
			ServerOptions: ServerOptions{
				SupportedCommands: commands,
			},
			UserOptions: UserOptions{
				BuildOptions: BuildOptions{
					ExpandWorkspaceToModule: true,
					DirectoryFilters:        []string{"-**/node_modules"},
					StandaloneTags:          []string{"ignore"},
				},
				UIOptions: UIOptions{
					DiagnosticOptions: DiagnosticOptions{
						Annotations: map[Annotation]bool{
							Bounds: true,
							Escape: true,
							Inline: true,
							Nil:    true,
						},
						DiagnosticsDelay:          1 * time.Second,
						DiagnosticsTrigger:        DiagnosticsOnEdit,
						AnalysisProgressReporting: true,
					},
					InlayHintOptions: InlayHintOptions{},
					DocumentationOptions: DocumentationOptions{
						HoverKind:    FullDocumentation,
						LinkTarget:   "pkg.go.dev",
						LinksInHover: true,
					},
					NavigationOptions: NavigationOptions{
						ImportShortcut: BothShortcuts,
						SymbolMatcher:  SymbolFastFuzzy,
						SymbolStyle:    DynamicSymbols,
						SymbolScope:    AllSymbolScope,
					},
					CompletionOptions: CompletionOptions{
						Matcher:                        Fuzzy,
						CompletionBudget:               100 * time.Millisecond,
						ExperimentalPostfixCompletions: true,
						CompleteFunctionCalls:          true,
					},
					Codelenses: map[string]bool{
						string(command.Generate):          true,
						string(command.RegenerateCgo):     true,
						string(command.Tidy):              true,
						string(command.GCDetails):         false,
						string(command.UpgradeDependency): true,
						string(command.Vendor):            true,
					},
				},
			},
			InternalOptions: InternalOptions{
				CompleteUnimported:          true,
				CompletionDocumentation:     true,
				DeepCompletion:              true,
				SubdirWatchPatterns:         SubdirWatchPatternsAuto,
				ReportAnalysisProgressAfter: 5 * time.Second,
				TelemetryPrompt:             false,
				LinkifyShowMessage:          false,
				IncludeReplaceInWorkspace:   true,
				ZeroConfig:                  true,
			},
			Hooks: Hooks{
				URLRegexp:            urlRegexp(),
				DefaultAnalyzers:     analyzers(),
				StaticcheckAnalyzers: map[string]*Analyzer{},
			},
		}
	})
	options := defaultOptions.Clone()
	for _, override := range overrides {
		if override != nil {
			override(options)
		}
	}
	return options
}
