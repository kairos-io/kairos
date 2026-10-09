package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

func testConfig() sdkConfig.Config {
	return sdkConfig.Config{Logger: sdkLogger.NewBufferLogger(&bytes.Buffer{})}
}

// blkid and dmsetup both write diagnostics to stderr while still succeeding.
// Merging those into the value makes the mapper path a sentence rather than a
// device, so the partition reads as locked and the encryption hook gives up.
func TestFindMapperDeviceIgnoresStderr(t *testing.T) {
	dir := t.TempDir()
	write := func(name, script string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatalf("could not write the fake %s: %v", name, err)
		}
	}
	write("blkid", `echo "blkid: cannot open /dev/sr0: No medium found" >&2
echo /dev/vda2`)
	write("dmsetup", `echo "device-mapper: version ioctl on failed: No such device" >&2
echo "vda2	(252:1)"`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := findMapperDeviceForPartition(testConfig(), "COS_PERSISTENT")
	if err != nil {
		t.Fatalf("findMapperDeviceForPartition failed although both commands succeeded: %v", err)
	}
	if got != "/dev/mapper/vda2" {
		t.Fatalf("findMapperDeviceForPartition returned %q, want /dev/mapper/vda2", got)
	}
}

func TestFindMapperDeviceReportsALockedPartition(t *testing.T) {
	dir := t.TempDir()
	for name, script := range map[string]string{
		"blkid":   `echo /dev/vda2`,
		"dmsetup": `echo "No devices found"`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatalf("could not write the fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := findMapperDeviceForPartition(testConfig(), "COS_PERSISTENT"); err == nil {
		t.Fatal("findMapperDeviceForPartition accepted a partition that is not unlocked")
	}
}

// preparePartitionsForEncryption looks the device up by label and then asks
// findmnt which mount points use it. A stderr line in the device path makes
// findmnt match nothing, so the partition is encrypted while still mounted.
func TestPreparePartitionsLooksUpTheRealDevice(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "findmnt.args")
	for name, script := range map[string]string{
		"blkid":   `echo "blkid: cannot open /dev/sr0: No medium found" >&2` + "\n" + `echo /dev/vda2`,
		"findmnt": `echo "$@" >> ` + seen,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatalf("could not write the fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := preparePartitionsForEncryption(testConfig(), []string{"COS_PERSISTENT"}); err != nil {
		t.Fatalf("preparePartitionsForEncryption failed: %v", err)
	}

	args, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("findmnt was never called: %v", err)
	}
	if !strings.Contains(string(args), "-S /dev/vda2") {
		t.Fatalf("findmnt was asked about the wrong device: %q", strings.TrimSpace(string(args)))
	}
}
