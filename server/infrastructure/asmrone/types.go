package asmrone

import (
	"sort"
	"strings"
)

// VoiceActor represents a voice actor credited on a work.
type VoiceActor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Tag represents a content tag on a work.
type Tag struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Circle represents the creator circle/group of a work.
type Circle struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// WorkInfo contains metadata for an asmr.one work.
type WorkInfo struct {
	ID                int          `json:"id"`
	Title             string       `json:"title"`
	Name              string       `json:"name"`              // circle/creator name (flat)
	SourceID          string       `json:"source_id"`         // e.g. "RJ299717"
	HasSubtitle       bool         `json:"has_subtitle"`      // true when the work ships .vtt files
	CircleInfo        Circle       `json:"circle"`            // structured circle info
	VAs               []VoiceActor `json:"vas"`               // voice actors
	Tags              []Tag        `json:"tags"`              // content tags
	MainCoverURL      string       `json:"mainCoverUrl"`      // full-size cover
	SamCoverURL       string       `json:"samCoverUrl"`       // small cover
	ThumbnailCoverURL string       `json:"thumbnailCoverUrl"` // 240x240 thumbnail
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

// AudioVTTPair bundles an audio track with its paired subtitle track.
type AudioVTTPair struct {
	Audio Track
	VTT   Track
}

// FindVTTPeer returns the .vtt sibling whose name matches "<audio>.vtt",
// or nil if none exists.
func (t Track) FindVTTPeer(siblings []Track) *Track {
	want := strings.ToLower(t.Title) + ".vtt"
	for i, s := range siblings {
		if strings.ToLower(s.Title) == want {
			return &siblings[i]
		}
	}
	return nil
}

// HasVTTPair reports whether this audio track has a sibling .vtt file whose
// name matches "<track.Title>.vtt" (case-insensitive).
func (t Track) HasVTTPair(siblings []Track) bool {
	if !t.IsAudio() {
		return false
	}
	return t.FindVTTPeer(siblings) != nil
}

// HasSubtitledAudio reports whether the track list contains at least one audio
// file with a paired .vtt sibling, searching the whole subtree recursively.
func HasSubtitledAudio(tracks []Track) bool {
	for _, t := range tracks {
		if t.IsAudio() && t.HasVTTPair(tracks) {
			return true
		}
		if t.IsFolder() && HasSubtitledAudio(t.Children) {
			return true
		}
	}
	return false
}

// SubtitledBrowseItems returns the items that should appear in the browse UI
// when subtitle-filtering is active:
//   - folders that contain at least one subtitled audio anywhere in their subtree
//   - audio files that have a matching .vtt sibling in the same directory
//
// Results are sorted alphabetically: folders first (A–Z), then audio files (A–Z).
func SubtitledBrowseItems(tracks []Track) []Track {
	var folders, audios []Track
	for _, t := range tracks {
		if t.IsFolder() && HasSubtitledAudio(t.Children) {
			folders = append(folders, t)
		} else if t.IsAudio() && t.HasVTTPair(tracks) {
			audios = append(audios, t)
		}
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Title < folders[j].Title })
	sort.Slice(audios, func(i, j int) bool { return audios[i].Title < audios[j].Title })
	return append(folders, audios...)
}

// SubtitledAudioInDir returns all audio files in the given (flat) directory
// that have a matching .vtt sibling – used by the "Select All" action.
func SubtitledAudioInDir(tracks []Track) []Track {
	var out []Track
	for _, t := range tracks {
		if t.IsAudio() && t.HasVTTPair(tracks) {
			out = append(out, t)
		}
	}
	return out
}
