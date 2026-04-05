package cli

import (
	"context"

	"github.com/banksean/sand/internal/daemon"
	"github.com/posener/complete"
)

type sandboxNamePredictor struct {
	mc daemon.Client
}

// Predict implements [complete.Predictor].
func (s *sandboxNamePredictor) Predict(args complete.Args) []string {
	sandboxes, err := s.mc.ListSandboxes(context.Background())
	if err != nil {
		return nil
	}
	ret := []string{}
	for _, box := range sandboxes {
		ret = append(ret, box.ID)
	}
	return ret
}

func NewSandboxNamePredictor(mc daemon.Client) complete.Predictor {
	return &sandboxNamePredictor{mc: mc}
}
