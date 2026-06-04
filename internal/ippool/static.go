package ippool

type static struct {
	pool []string
}

func NewStatic(ips []string) Pool {
	return &static{
		pool: ips,
	}
}

func (s *static) Get() (string, error) {
	return s.pool[0], nil
}
