package backend

type Backend interface {
	Sync(req *Request) error
}

type Request struct {
	ServiceName string
	Ips         []string
	LbIp        string
	Protocol    string
	Port        int32
	NodePort    int32
}
