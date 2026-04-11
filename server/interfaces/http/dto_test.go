package http

import "testing"

func TestCreateJobRequestToJobConfigDefaultsFilterFalse(t *testing.T) {
	req := CreateJobRequest{
		TTSProvider: "edge",
		TTSVolume:   0.08,
		NoSpeedup:   false,
		Concurrency: 3,
	}

	cfg := req.ToJobConfig()
	if cfg.FilterOnomatopoeia {
		t.Fatalf("expected FilterOnomatopoeia=false by default")
	}
}

func TestCreateJobRequestToJobConfigMapsFilter(t *testing.T) {
	req := CreateJobRequest{
		TTSProvider:        "edge",
		TTSVolume:          0.08,
		NoSpeedup:          true,
		FilterOnomatopoeia: true,
		Concurrency:        3,
	}

	cfg := req.ToJobConfig()
	if !cfg.FilterOnomatopoeia {
		t.Fatalf("expected FilterOnomatopoeia=true")
	}
}
