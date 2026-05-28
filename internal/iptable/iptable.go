package iptable

import (
	"fmt"
	"os/exec"
)

type Iptable interface {
	Exec(arg ...string) error
	Delete(arg ...string) error
}

type iptable struct {
}

func NewIptable() Iptable {
	return &iptable{}
}

func (i *iptable) Exec(arg ...string) error {
	cmd := exec.Command("iptables", arg...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("error: %v\n", err)
		fmt.Printf("output: %s\n", string(output))
		return err
	}
	return nil
}

func (i *iptable) Delete(arg ...string) error {
	//TODO implement me
	panic("implement me")
}
