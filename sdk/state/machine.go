package state

type Machine struct {
	UUID        string
	BootArgs    []string
	CloudConfig string
}

type Spec struct {
	MachineSpec Machine
}
