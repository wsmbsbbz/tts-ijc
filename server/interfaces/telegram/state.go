package telegram

import (
	"sync"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

type convState int

const (
	stateIdle convState = iota
	stateWaitingAudio
	stateWaitingVTT
	stateWaitingConfig
	stateConfirming
)

// configStep constants track sub-steps within stateWaitingConfig.
const (
	configStepProvider = 0
	configStepVolume   = 1
	configStepSpeedup  = 2
)

type session struct {
	state       convState
	configStep  int
	userID      string // domain.User.ID
	audioFileID string
	audioName   string
	audioSize   int64
	vttFileID   string
	vttName     string
	vttSize     int64
	cfg         domain.JobConfig
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
