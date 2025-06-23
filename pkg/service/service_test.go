package service

import (
	"KubeDiskGuard/pkg/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseAnnotations(t *testing.T) {
	defaultReadIOPS := 500
	defaultWriteIOPS := 500
	defaultReadBPS := 10485760  // 10M
	defaultWriteBPS := 10485760 // 10M
	prefix := "io-limit"
	customPrefix := "custom.io"

	cases := []struct {
		name              string
		annotations       map[string]string
		prefix            string
		expectedReadIops  int
		expectedWriteIops int
		expectedReadBps   int
		expectedWriteBps  int
	}{
		{
			name:              "No annotations, use default",
			annotations:       map[string]string{},
			prefix:            prefix,
			expectedReadIops:  defaultReadIOPS,
			expectedWriteIops: defaultWriteIOPS,
			expectedReadBps:   defaultReadBPS,
			expectedWriteBps:  defaultWriteBPS,
		},
		{
			name: "Smart limit annotations with custom prefix",
			annotations: map[string]string{
				customPrefix + "/read-iops":  "100",
				customPrefix + "/write-iops": "150",
				customPrefix + "/read-bps":   "200K",
				customPrefix + "/write-bps":  "250M",
			},
			prefix:            customPrefix,
			expectedReadIops:  100,
			expectedWriteIops: 150,
			expectedReadBps:   200 * 1024,
			expectedWriteBps:  250 * 1024 * 1024,
		},
		{
			name: "Limit removed annotation has top priority",
			annotations: map[string]string{
				prefix + "/removed":   "true",
				prefix + "/read-iops": "100",
			},
			prefix:            prefix,
			expectedReadIops:  0,
			expectedWriteIops: 0,
			expectedReadBps:   0,
			expectedWriteBps:  0,
		},
		{
			name: "Legacy nvme annotations",
			annotations: map[string]string{
				"nvme-iops": "2",
				"nvme-bps":  "3M",
			},
			prefix:            prefix,
			expectedReadIops:  2,
			expectedWriteIops: 2,
			expectedReadBps:   3 * 1024 * 1024,
			expectedWriteBps:  3 * 1024 * 1024,
		},
		{
			name: "Smart limit has priority over legacy",
			annotations: map[string]string{
				prefix + "/read-iops": "100",
				"nvme-iops":           "200",
				"nvme-bps":            "5M",
			},
			prefix:            prefix,
			expectedReadIops:  100,
			expectedWriteIops: defaultWriteIOPS, // as write iops is not set via smart limit
			expectedReadBps:   defaultReadBPS,   // as bps is not set via smart limit
			expectedWriteBps:  defaultWriteBPS,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svcConfig := &config.Config{
				ContainerReadIOPSLimit:     defaultReadIOPS,
				ContainerWriteIOPSLimit:    defaultWriteIOPS,
				ContainerReadBPSLimit:      defaultReadBPS,
				ContainerWriteBPSLimit:     defaultWriteBPS,
				SmartLimitAnnotationPrefix: tc.prefix,
			}

			readIops, writeIops := ParseIopsLimitFromAnnotations(tc.annotations, svcConfig.ContainerReadIOPSLimit, svcConfig.ContainerWriteIOPSLimit, svcConfig.SmartLimitAnnotationPrefix)
			assert.Equal(t, tc.expectedReadIops, readIops, "Read IOPS should match")
			assert.Equal(t, tc.expectedWriteIops, writeIops, "Write IOPS should match")

			readBps, writeBps := ParseBpsLimitFromAnnotations(tc.annotations, svcConfig.ContainerReadBPSLimit, svcConfig.ContainerWriteBPSLimit, svcConfig.SmartLimitAnnotationPrefix)
			assert.Equal(t, tc.expectedReadBps, readBps, "Read BPS should match")
			assert.Equal(t, tc.expectedWriteBps, writeBps, "Write BPS should match")
		})
	}
}

func TestShouldSkipContainer(t *testing.T) {
	svc := &KubeDiskGuardService{
		Config: &config.Config{
			ExcludeKeywords: []string{"pause", "istio-proxy"},
		},
	}

	testCases := []struct {
		name     string
		image    string
		cname    string
		expected bool
	}{
		{"no exclusion", "my-app:latest", "main-container", false},
		{"image excluded", "my-pause-image:latest", "main-container", true},
		{"name excluded", "my-app:latest", "istio-proxy-sidecar", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, svc.ShouldSkipContainer(tc.image, tc.cname))
		})
	}
}
