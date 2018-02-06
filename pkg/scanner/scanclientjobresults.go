package scanner

import (
	"time"

	"bitbucket.org/bdsengineering/perceptor/pkg/docker"
)

type ScanClientJobResults struct {
	DockerStats        docker.ImagePullStats
	ScanClientDuration *time.Duration
	TotalDuration      *time.Duration
	Err                *ScanError
}
