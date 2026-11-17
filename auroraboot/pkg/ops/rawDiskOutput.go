package ops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/utils"

	uuidPkg "github.com/gofrs/uuid"
	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
)

// Raw2Azure converts a raw disk to a VHD disk compatible with Azure
// All VHDs on Azure must have a virtual size aligned to 1 MB (1024 × 1024 bytes)
// The Hyper-V virtual hard disk (VHDX) format isn't supported in Azure, only fixed VHD
func Raw2Azure(source string) (string, error) {
	internal.Log.Logger.Info().Str("source", source).Msg("Converting raw disk to Azure VHD")
	name := fmt.Sprintf("%s.vhd", source)
	// Copy raw to new image with VHD appended
	// rename file to .vhd
	err := os.Rename(source, name)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error renaming raw image to vhd")
		return name, err
	}
	// Open it
	vhdFile, _ := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, constants.FilePerm)
	// Calculate rounded size
	info, err := vhdFile.Stat()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error getting file info")
		return name, err
	}
	actualSize := info.Size()
	var finalSize int64
	// Calculate the final size in bytes
	finalSize = ((actualSize + constants.MB - 1) / constants.MB) * constants.MB

	// If the actual size is different from the final size, we have to resize the image
	if actualSize != finalSize {
		internal.Log.Logger.Info().Int64("actualSize", actualSize).Int64("finalSize", finalSize+512).Msg("Resizing image")
		// If you do not seek, you will override the data
		_, err = vhdFile.Seek(0, io.SeekEnd)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error seeking to end")
			return name, err
		}
		err = vhdFile.Truncate(finalSize)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error truncating file")
			return name, err
		}
	}
	// Transform it to VHD
	info, err = vhdFile.Stat() // Stat again to get the new size
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error getting file info")
		return name, err
	}
	size := uint64(info.Size())
	header := newVHDFixed(size)
	err = binary.Write(vhdFile, binary.BigEndian, header)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error writing header")
		return name, err
	}
	err = vhdFile.Close()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error closing file")
		return name, err
	}
	// Lets validate that the file size is divisible by 1 MB before claiming its ok
	fileInfo, err := os.Stat(vhdFile.Name())
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error getting file info")
		return name, err
	}
	sizeFile := fileInfo.Size()

	if int64(size)%constants.MB != 0 {
		err = fmt.Errorf("The file %s size %d bytes is not divisible by 1 MB.\n", fileInfo.Name(), sizeFile)
		internal.Log.Logger.Error().Err(err).Msg("Error validating file size")
		return name, err

	}
	return name, nil
}

// Raw2Gce transforms an image from RAW format into GCE format
// The RAW image file must have a size in an increment of 1 GB. For example, the file must be either 10 GB or 11 GB but not 10.5 GB.
// The disk image filename must be disk.raw.
// The compressed file must be a .tar.gz file that uses gzip compression and the --format=oldgnu option for the tar utility.
func Raw2Gce(source string) (string, error) {
	internal.Log.Logger.Info().Msg("Transforming raw image into gce format")
	name := fmt.Sprintf("%s.gce.tar.gz", source)
	actImg, err := os.OpenFile(source, os.O_CREATE|os.O_APPEND|os.O_WRONLY, constants.FilePerm)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", source).Msg("Error opening file")
		return name, err
	}
	info, err := actImg.Stat()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", source).Msg("Error getting file info")
		return name, err
	}
	actualSize := info.Size()
	finalSizeGB := actualSize/constants.GB + 1
	finalSizeBytes := finalSizeGB * constants.GB
	internal.Log.Logger.Info().Int64("current", actualSize).Int64("final", finalSizeBytes).Str("file", source).Msg("Resizing image")
	// REMEMBER TO SEEK!
	_, err = actImg.Seek(0, io.SeekEnd)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", source).Msg("Error seeking to end")
		return name, err
	}
	err = actImg.Truncate(finalSizeBytes)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", source).Msg("Error truncating file")
		return name, err
	}
	err = actImg.Close()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("file", source).Msg("Error closing file")
		return name, err
	}

	// Tar gz the image

	// Create destination file
	file, err := os.Create(name)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("destination", name).Msg("Error creating destination file")
		return name, err
	}
	internal.Log.Logger.Info().Str("destination", file.Name()).Msg("Compressing raw image into a tar.gz")

	defer func(file *os.File) {
		err = file.Close()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("destination", file.Name()).Msg("Error closing destination file")
		}
	}(file)
	// Create gzip writer
	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestSpeed)
	if err != nil {
		return name, err
	}
	defer func(gzipWriter *gzip.Writer) {
		err := gzipWriter.Close()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("destination", file.Name()).Msg("Error closing gzip writer")
		}
	}(gzipWriter)
	// Create tarwriter pointing to our gzip writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer func(tarWriter *tar.Writer) {
		err = tarWriter.Close()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("destination", file.Name()).Msg("Error closing tar writer")
		}
	}(tarWriter)

	// Open disk.raw
	sourceFile, _ := os.Open(source)
	sourceStat, _ := sourceFile.Stat()
	defer func(sourceFile fs.File) {
		err = sourceFile.Close()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error closing source file")
		}
	}(sourceFile)

	// Add disk.raw file
	header := &tar.Header{
		Name:   "disk.raw",
		Size:   sourceStat.Size(),
		Mode:   int64(sourceStat.Mode()),
		Format: tar.FormatGNU,
	}
	// Write header with all the info
	err = tarWriter.WriteHeader(header)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error writing header")
		return name, err
	}
	// copy the actual data
	_, err = io.Copy(tarWriter, sourceFile)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error copying data")
		return name, err
	}
	// Remove full raw image, we already got the compressed one
	err = os.RemoveAll(source)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", source).Msg("Error removing full raw image")
		return name, err
	}
	return name, nil
}

/// VHD utils!

type VHDHeader struct {
	Cookie             [8]byte   // Cookies are used to uniquely identify the original creator of the hard disk image
	Features           [4]byte   // This is a bit field used to indicate specific feature support. Can be 0x00000000 (no features), 0x00000001 (Temporary, candidate for deletion on shutdown) or 0x00000002 (Reserved)
	FileFormatVersion  [4]byte   // Divided into a major/minor version and matches the version of the specification used in creating the file.
	DataOffset         [8]byte   // For fixed disks, this field should be set to 0xFFFFFFFF.
	Timestamp          [4]byte   // Stores the creation time of a hard disk image. This is the number of seconds since January 1, 2000 12:00:00 AM in UTC/GMT.
	CreatorApplication [4]byte   // Used to document which application created the hard disk.
	CreatorVersion     [4]byte   // This field holds the major/minor version of the application that created the hard disk image.
	CreatorHostOS      [4]byte   // This field stores the type of host operating system this disk image is created on.
	OriginalSize       [8]byte   // This field stores the size of the hard disk in bytes, from the perspective of the virtual machine, at creation time. Info only
	CurrentSize        [8]byte   // This field stores the current size of the hard disk, in bytes, from the perspective of the virtual machine.
	DiskGeometry       [4]byte   // This field stores the cylinder, heads, and sectors per track value for the hard disk.
	DiskType           [4]byte   // Fixed = 2, Dynamic = 3, Differencing = 4
	Checksum           [4]byte   // This field holds a basic checksum of the hard disk footer. It is just a one’s complement of the sum of all the bytes in the footer without the checksum field.
	UniqueID           [16]byte  // This is a 128-bit universally unique identifier (UUID).
	SavedState         [1]byte   // This field holds a one-byte flag that describes whether the system is in saved state. If the hard disk is in the saved state the value is set to 1
	Reserved           [427]byte // This field contains zeroes.
}

// Lots of magic numbers here, but they are all defined in the VHD format spec
func newVHDFixed(size uint64) VHDHeader {
	header := VHDHeader{}
	hexToField("636f6e6563746978", header.Cookie[:])
	hexToField("00000002", header.Features[:])
	hexToField("00010000", header.FileFormatVersion[:])
	hexToField("ffffffffffffffff", header.DataOffset[:])
	t := uint32(time.Now().Unix() - 946684800)
	binary.BigEndian.PutUint32(header.Timestamp[:], t)
	hexToField("656c656d", header.CreatorApplication[:]) // Cos
	hexToField("73757365", header.CreatorHostOS[:])      // SUSE
	binary.BigEndian.PutUint64(header.OriginalSize[:], size)
	binary.BigEndian.PutUint64(header.CurrentSize[:], size)
	// Divide size into 512 to get the total sectors
	totalSectors := float64(size / 512)
	geometry := chsCalculation(uint64(totalSectors))
	binary.BigEndian.PutUint16(header.DiskGeometry[:2], uint16(geometry.cylinders))
	header.DiskGeometry[2] = uint8(geometry.heads)
	header.DiskGeometry[3] = uint8(geometry.sectorsPerTrack)
	hexToField("00000002", header.DiskType[:]) // Fixed 0x00000002
	hexToField("00000000", header.Checksum[:])
	uuid, _ := uuidPkg.NewV4() // Generate a new UUID v4!
	copy(header.UniqueID[:], uuid.String())
	generateChecksum(&header)
	return header
}

// generateChecksum generates the checksum of the vhd header
// Lifted from the official VHD Format Spec
func generateChecksum(header *VHDHeader) {
	buffer := new(bytes.Buffer)
	_ = binary.Write(buffer, binary.BigEndian, header)
	checksum := 0
	bb := buffer.Bytes()
	for counter := 0; counter < 512; counter++ {
		checksum += int(bb[counter])
	}
	binary.BigEndian.PutUint32(header.Checksum[:], uint32(^checksum))
}

// hexToField decodes an hex to bytes and copies it to the given header field
func hexToField(hexs string, field []byte) {
	h, _ := hex.DecodeString(hexs)
	copy(field, h)
}

// chs is a simple struct to represent the cylinders/heads/sectors for a given sector count
type chs struct {
	cylinders       uint
	heads           uint
	sectorsPerTrack uint
}

// chsCalculation calculates the cylinders, headers and sectors per track for a given sector count
// Exactly the same code on the official VHD format spec
func chsCalculation(sectors uint64) chs {
	var sectorsPerTrack,
		heads,
		cylinderTimesHeads,
		cylinders float64
	totalSectors := float64(sectors)

	if totalSectors > 65535*16*255 {
		totalSectors = 65535 * 16 * 255
	}

	if totalSectors >= 65535*16*63 {
		sectorsPerTrack = 255
		heads = 16
		cylinderTimesHeads = math.Floor(totalSectors / sectorsPerTrack)
	} else {
		sectorsPerTrack = 17
		cylinderTimesHeads = math.Floor(totalSectors / sectorsPerTrack)
		heads = math.Floor((cylinderTimesHeads + 1023) / 1024)
		if heads < 4 {
			heads = 4
		}
		if (cylinderTimesHeads >= (heads * 1024)) || heads > 16 {
			sectorsPerTrack = 31
			heads = 16
			cylinderTimesHeads = math.Floor(totalSectors / sectorsPerTrack)
		}
		if cylinderTimesHeads >= (heads * 1024) {
			sectorsPerTrack = 63
			heads = 16
			cylinderTimesHeads = math.Floor(totalSectors / sectorsPerTrack)
		}
	}

	cylinders = cylinderTimesHeads / heads

	return chs{
		cylinders:       uint(cylinders),
		heads:           uint(heads),
		sectorsPerTrack: uint(sectorsPerTrack),
	}
}

// Model specific functions

// copyFirmwareRpi will copy the proper firmware files for a Raspberry Pi into the EFI partition
func copyFirmwareRpi(target string) error {
	internal.Log.Logger.Info().Str("target", target).Msg("Copying Raspberry Pi firmware")
	// Copy the firmware files from /rpi/ into target
	return utils.CopyDir("/arm/rpi/", target)
}
