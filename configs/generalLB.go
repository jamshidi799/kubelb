package configs

import "time"

type GeneralLBConfig struct {
	Protocol string
	NodesIp  []string
	Services []Service
}

type Service struct {
	Name        string
	LbPort      int
	NodePort    int
	HealthCheck *HealthCheck
}

type HealthCheck struct {
	Probe            string
	Path             string
	ExpectedStatus   int
	HttpHeaders      map[string]string
	SuccessThreshold int
	FailureThreshold int
	Timeout          int
	Interval         time.Duration
}
