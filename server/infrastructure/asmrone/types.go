package asmrone

import "strings"

// WorkInfo contains metadata for an asmr.one work.
type WorkInfo struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Name     string `json:"name"`      // circle/creator name
	SourceID string `json:"source_id"` // e.g. "RJ299717"
}

// Track represents a file or folder in an asmr.one work's file tree.
type Track struct {
	Type             string  `json:"type"`
	Title            string  `json:"title"`
	MediaDownloadURL string  `json:"mediaDownloadUrl"`
	FileSize         int64   `json:"fileSize"`
	Duration         float64 `json:"duration"` // seconds
	Children         []Track `json:"children"`
}

// IsAudio reports whether this track is an audio file.
func (t Track) IsAudio() bool {
	if t.Type == "audio" {
		return true
	}
	lower := strings.ToLower(t.Title)
	for _, ext := range []string{".mp3", ".wav", ".ogg", ".flac", ".m4a", ".aac", ".opus"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// IsVTT reports whether this track is a WebVTT subtitle file.
func (t Track) IsVTT() bool {
	return strings.HasSuffix(strings.ToLower(t.Title), ".vtt")
}

// IsFolder reports whether this track is a directory.
func (t Track) IsFolder() bool {
	return t.Type == "folder"
}

// FlattenVTTs returns all VTT tracks found anywhere in the tree (depth-first).
func FlattenVTTs(tracks []Track) []Track {
	var out []Track
	for _, t := range tracks {
		if t.IsVTT() {
			out = append(out, t)
		}
		if t.IsFolder() {
			out = append(out, FlattenVTTs(t.Children)...)
		}
	}
	return out
}

// BrowseItems filters tracks to only folders and audio files (items shown in browse UI).
func BrowseItems(tracks []Track) []Track {
	var out []Track
	for _, t := range tracks {
		if t.IsFolder() || t.IsAudio() {
			out = append(out, t)
		}
	}
	return out
}
