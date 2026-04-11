package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

func newTestBotServer(t *testing.T) *BotServer {
	t.Helper()
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(apiSrv.Close)

	b := &BotServer{
		api:   newTGAPI("test-token", apiSrv.URL),
		store: newStateStore(),
	}
	return b
}

func TestRJConfigFlowGoesToOnomatopoeiaStep(t *testing.T) {
	b := newTestBotServer(t)
	chatID := int64(123)
	sess := &session{
		state:       stateWaitingConfig,
		configStep:  configStepSpeedup,
		rjMode:      true,
		configMsgID: 1,
		cfg: domain.JobConfig{
			TTSProvider: "edge",
			TTSVolume:   0.08,
		},
	}
	b.store.set(chatID, sess)

	b.handleSpeedupCallback(context.Background(), chatID, sess, "on")

	if sess.state != stateWaitingConfig {
		t.Fatalf("expected stateWaitingConfig, got %v", sess.state)
	}
	if sess.configStep != configStepOnomatopoeia {
		t.Fatalf("expected configStepOnomatopoeia, got %d", sess.configStep)
	}
}

func TestOnomatopoeiaStepTransitionsToConfirming(t *testing.T) {
	b := newTestBotServer(t)
	chatID := int64(123)
	sess := &session{
		state:       stateWaitingConfig,
		configStep:  configStepOnomatopoeia,
		rjMode:      true,
		configMsgID: 1,
		cfg: domain.JobConfig{
			TTSProvider: "edge",
			TTSVolume:   0.08,
		},
	}
	b.store.set(chatID, sess)

	b.handleOnomatopoeiaCallback(context.Background(), chatID, sess, "on")

	if sess.state != stateConfirming {
		t.Fatalf("expected stateConfirming, got %v", sess.state)
	}
	if !sess.cfg.FilterOnomatopoeia {
		t.Fatalf("expected FilterOnomatopoeia enabled")
	}
}
