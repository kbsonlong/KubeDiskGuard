package service

import (
	"KubeDiskGuard/pkg/config"
	"testing"
)

func TestShouldSkipContainer(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		cname    string
		keywords []string
		expect   bool
	}{
		{"skip by image", "nginx:pause", "nginx", []string{"pause"}, true},
		{"skip by name", "nginx:latest", "pause-container", []string{"pause"}, true},
		{"not skip", "nginx:latest", "nginx", []string{"pause"}, false},
		{"skip exact", "pause", "pause", []string{"pause"}, true},
		{"skip by suffix", "nginx-proxy", "nginx-proxy", []string{"proxy"}, true},
		{"skip by prefix", "istio-proxy", "istio-proxy", []string{"istio"}, true},
	}
	svc := &KubeDiskGuardService{config: &config.Config{}}
	for _, tt := range tests {
		svc.config.ExcludeKeywords = tt.keywords
		result := svc.ShouldSkipContainer(tt.image, tt.cname)
		if result != tt.expect {
			t.Errorf("%s: ShouldSkipContainer(%q, %q) = %v, want %v", tt.name, tt.image, tt.cname, result, tt.expect)
		}
	}
}
