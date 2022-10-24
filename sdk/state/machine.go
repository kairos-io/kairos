package state

type Machine struct {
	BootArgs    []string
	CloudConfig string
}

type Spec struct {
	MachineSpec Machine
}
