package domain

import "context"

// TranslateInput contains the local file paths and config for a translation run.
type TranslateInput struct {
	AudioPath  string
	VTTPath    string
	OutputPath string
	Config     JobConfig
}

// TranslateProgress reports the current progress of a translation run.
type TranslateProgress struct {
	Current int
	Total   int
	Message string
}

// Translator executes the audio translation process.
type Translator interface {
	Execute(ctx context.Context, input TranslateInput, onProgress func(TranslateProgress)) error
}
