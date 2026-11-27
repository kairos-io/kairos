package p2p

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

const (
	// DefaultChunkSize is the size chunks are hashed and fetched in.
	DefaultChunkSize = 4 << 20
	// minChunkSize bounds the number of requests a manifest can make a
	// downloader do for a given image.
	minChunkSize = 256 << 10
	maxChunkSize = 64 << 20
	// maxChunks bounds the work a manifest can ask for.
	maxChunks = 16384
	// maxManifestBytes bounds the manifest a peer can make us parse.
	maxManifestBytes = 2 << 20
	maxImageSize     = 1 << 42
)

// Manifest describes one image: its size and the sha256 of every chunk and of
// the whole image. The seeder always computes it itself from the file.
type Manifest struct {
	Version   int      `json:"version"`
	Image     string   `json:"image"`
	Size      int64    `json:"size"`
	ChunkSize int64    `json:"chunkSize"`
	SHA256    string   `json:"sha256"`
	Chunks    []string `json:"chunks"`
}

// NumChunks returns how many chunks the image has.
func (m *Manifest) NumChunks() int { return len(m.Chunks) }

// ChunkLen returns the length of chunk i (the last one may be short).
func (m *Manifest) ChunkLen(i int) int64 {
	if rem := m.Size - int64(i)*m.ChunkSize; rem < m.ChunkSize {
		return rem
	}
	return m.ChunkSize
}

// Root is the sha256 over a canonical encoding of the whole manifest: the image
// kind, the size, the chunk size, the hash of the image and the ordered chunk
// hashes. It is the "pin" of a source (?pin=): a manifest is the right one if,
// and only if, its root is the pin, which authenticates every chunk hash before
// a single chunk is fetched.
func (m *Manifest) Root() string {
	h := sha256.New()
	fmt.Fprintf(h, "kairos-p2p-manifest-root-v1\n%s\n%d\n%d\n%s\n", m.Image, m.Size, m.ChunkSize, m.SHA256)
	for _, c := range m.Chunks {
		h.Write([]byte(c))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ID identifies the exact manifest: peers serving another one are another image.
func (m *Manifest) ID() string { return m.Root() }

func validHex256(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && s == hex.EncodeToString(b)
}

// Validate checks a manifest received from a peer. Nothing in it is trusted.
func (m *Manifest) Validate() error {
	if m.Version != 1 {
		return fmt.Errorf("unsupported manifest version %d", m.Version)
	}
	if !images.ValidP2PImage(m.Image) {
		return fmt.Errorf("manifest names unknown image %q", m.Image)
	}
	if m.Size <= 0 || m.Size > maxImageSize {
		return fmt.Errorf("manifest size %d is out of range", m.Size)
	}
	if m.ChunkSize < minChunkSize || m.ChunkSize > maxChunkSize {
		return fmt.Errorf("manifest chunk size %d is out of range", m.ChunkSize)
	}
	want := int((m.Size + m.ChunkSize - 1) / m.ChunkSize)
	if want > maxChunks {
		return fmt.Errorf("manifest would need %d chunks, at most %d are accepted", want, maxChunks)
	}
	if len(m.Chunks) != want {
		return fmt.Errorf("manifest has %d chunk hashes, %d expected", len(m.Chunks), want)
	}
	for i, c := range m.Chunks {
		if !validHex256(c) {
			return fmt.Errorf("manifest chunk %d hash is not a sha256", i)
		}
	}
	if !validHex256(m.SHA256) {
		return fmt.Errorf("manifest image hash is not a sha256")
	}
	return nil
}

// BuildManifest reads the file once and computes the chunk hashes and the hash
// of the whole image.
func BuildManifest(path, image string, chunkSize int64) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	return BuildManifestFrom(f, image, chunkSize)
}

// BuildManifestFrom is BuildManifest over an open file. The chunk hashes and the
// hash of the whole image come from the same pass over the file, so the
// manifest is always self-consistent.
func BuildManifestFrom(f *os.File, image string, chunkSize int64) (*Manifest, error) {
	if chunkSize == 0 {
		chunkSize = DefaultChunkSize
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 {
		return nil, fmt.Errorf("%s is not a non-empty regular file", f.Name())
	}
	m := &Manifest{Version: 1, Image: image, Size: st.Size(), ChunkSize: chunkSize}
	whole := sha256.New()
	buf := make([]byte, chunkSize)
	for off := int64(0); off < m.Size; off += chunkSize {
		n := chunkSize
		if rem := m.Size - off; rem < n {
			n = rem
		}
		if _, err := io.ReadFull(io.NewSectionReader(f, off, n), buf[:n]); err != nil {
			return nil, fmt.Errorf("reading %s: %w", f.Name(), err)
		}
		whole.Write(buf[:n])
		h := sha256.Sum256(buf[:n])
		m.Chunks = append(m.Chunks, hex.EncodeToString(h[:]))
	}
	m.SHA256 = hex.EncodeToString(whole.Sum(nil))
	return m, m.Validate()
}

// SHA256File returns the hex sha256 of a file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
