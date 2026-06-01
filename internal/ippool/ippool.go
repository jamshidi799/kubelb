package ippool

type Pool interface {
	Get() (string, error)
}
