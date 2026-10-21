package elemental

// SetPartLabelDevice replaces the /dev/disk/by-partlabel lookup used by
// PartitionAndFormatDevice and returns a restorer for the host lookup.
func SetPartLabelDevice(fn func(name string) (string, error)) func() {
	prev := partLabelDevice
	partLabelDevice = fn
	return func() { partLabelDevice = prev }
}
