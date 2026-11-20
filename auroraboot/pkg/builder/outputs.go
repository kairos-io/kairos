package builder

import "fmt"

// ValidateOutputs rejects output formats that do not go with the boot mode.
// A Trusted Boot image (kairos-init -t) has no GRUB and no separate initrd:
// it boots from a signed UKI that the build packs the whole rootfs into.
// Netboot (extracted from the plain ISO) and every disk image (the raw disk
// path reads GRUB and its config from the rootfs) need what such an image does
// not have. The ISO is the exception: in Trusted Boot mode it is the UKI ISO,
// which is the installer of that mode. The error wraps ErrInvalidBuildOptions.
func (o BuildOptions) ValidateOutputs() error {
	trustedBoot := o.TrustedBoot || o.Outputs.TrustedBoot
	if !trustedBoot {
		return nil
	}
	for _, f := range []struct {
		field     string
		requested bool
	}{
		{"outputs.netboot", o.Netboot || o.Outputs.Netboot},
		{"outputs.rawDisk", o.Outputs.RawDisk},
		{"outputs.cloudImage", o.CloudImage || o.Outputs.CloudImage},
		{"outputs.gce", o.Outputs.GCE},
		{"outputs.vhd", o.Outputs.VHD},
		{"outputs.maas", o.Outputs.MAAS},
	} {
		if f.requested {
			return fmt.Errorf("%w: %s cannot be built from a Trusted Boot image, which ships as the UKI ISO and the tar", ErrInvalidBuildOptions, f.field)
		}
	}
	// The UKI step signs with all three keys and refuses to start without
	// them, which would otherwise surface only minutes into the build.
	s := o.Signing
	if (o.Outputs.UKIISO() || o.ISO) && (s.UKISecureBootKey == "" || s.UKISecureBootCert == "" || s.UKITPMPCRKey == "") {
		return fmt.Errorf("%w: a Trusted Boot ISO needs a SecureBoot key, certificate and TPM PCR key: set signing.ukiKeySetId to a key set that has all three", ErrInvalidBuildOptions)
	}
	return nil
}
