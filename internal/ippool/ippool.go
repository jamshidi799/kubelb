package ippool

type Pool interface {
	Get() (string, error)
	Take(ip string)
}
