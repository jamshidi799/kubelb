package ippool

import (
	"errors"
	"testing"
)

func TestStatic_Get(t *testing.T) {
	ips := []string{"1.1.1.1", "2.2.2.2"}
	p := NewStatic(ips)

	got, err := p.Get()
	if err != nil {
		t.Error(err)
	}

	if got != ips[0] {
		t.Error("Expected", ips[0], "got", got)
	}

	got, err = p.Get()
	if err != nil {
		t.Error(err)
	}

	if got != ips[1] {
		t.Error("Expected", ips[1], "got", got)
	}

	got, err = p.Get()
	if err == nil {
		t.Error("Expected error, got nil")
	}

	if !errors.Is(err, ErrNoAvailableIp) {
		t.Error("Expected", ErrNoAvailableIp, "got", err)
	}
}

func TestStatic_Take(t *testing.T) {
	ips := []string{"1.1.1.1"}
	p := NewStatic(ips)

	ip, _ := p.Get()
	p.Take(ip)

	got, err := p.Get()
	if err != nil {
		t.Error(err)
	}

	if got != ip {
		t.Error("Expected", ip, "got", got)
	}

}
