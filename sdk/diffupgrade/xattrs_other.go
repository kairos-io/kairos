//go:build !linux

package diffupgrade

// xattrs is not implemented off Linux, where no Kairos image is unpacked.
func xattrs(_ string) (map[string]string, error) {
	return nil, nil
}
