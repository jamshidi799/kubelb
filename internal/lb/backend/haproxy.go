package backend

type haproxy struct {
}

func NewHaproxyBackend() Backend {
	return &haproxy{}
}

func (l *haproxy) Sync(req *Request) error {
	//TODO implement me
	panic("implement me")
}
