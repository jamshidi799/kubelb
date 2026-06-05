package ippool

import "errors"

var ErrNoAvailableIp = errors.New("no available ip")

type static struct {
	pool []string
}

func NewStatic(ips []string) Pool {
	return &static{
		pool: ips,
	}
}

func (s *static) Get() (string, error) {
	if len(s.pool) > 0 {
		ip := s.pool[0]
		s.pool = s.pool[1:]
		return ip, nil
	}
	return "", ErrNoAvailableIp
}

func (s *static) Take(ip string) {
	s.pool = append(s.pool, ip)
}
