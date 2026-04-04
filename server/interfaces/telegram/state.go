package telegram

import (
	"sync"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
	"github.com/wsmbsbbz/tts-ijc/server/infrastructure/asmrone"
)

type convState int

const (
	stateIdle convState = iota
	stateWaitingAudio
	stateWaitingVTT
	stateWaitingConfig
	stateConfirming
	// RJ workflow states
	stateRJWaitingID // user sent /rj, waiting for RJ number input
	stateRJBrowse    // browsing file tree, multi-selecting audio files
)

// configStep constants track sub-steps within stateWaitingConfig.
const (
	configStepProvider = 0
	configStepVolume   = 1
	configStepSpeedup  = 2
)

// dirLevel is one level of the folder navigation stack.
type dirLevel struct {
	title  string          // folder title (for breadcrumb display)
	tracks []asmrone.Track // items in this folder
}

type session struct {
	state      convState
	configStep int
	userID     string // domain.User.ID
	// Upload-based workflow
	audioFileID string
	audioName   string
	audioSize   int64
	vttFileID   string
	vttName     string
	vttSize     int64
	cfg         domain.JobConfig
	// Config wizard message (shared by upload and RJ workflows)
	configMsgID int // message ID of the provider/volume/speedup wizard message
	// RJ workflow
	rjMode        bool            // true when job originates from asmr.one
	rjWorkno      string          // e.g. "RJ299717"
	rjWorkTitle   string          // human-readable title
	rjAsmrToken   string          // JWT token used for this session
	rjPath        []string        // folder breadcrumb titles
	rjDirStack    []dirLevel      // parent folders (for Back navigation)
	rjCurrentDir   []asmrone.Track                     // items in current view
	rjSelectedURLs map[string]asmrone.AudioVTTPair     // selected audio: URL → AudioVTTPair
	rjMenuMsgID    int                                 // message ID of the current selection keyboard
	rjWorkInfo     *asmrone.WorkInfo                   // rich metadata (cover, VAs, tags, circle)
}

type stateStore struct {
	mu       sync.Mutex
	sessions map[int64]*session // keyed by Telegram chat ID
}

func newStateStore() *stateStore {
	return &stateStore{sessions: make(map[int64]*session)}
}

func (s *stateStore) get(chatID int64) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[chatID]; ok {
		return sess
	}
	sess := &session{state: stateIdle}
	s.sessions[chatID] = sess
	return sess
}

func (s *stateStore) set(chatID int64, sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[chatID] = sess
}

func (s *stateStore) reset(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[chatID] = &session{state: stateIdle}
}
