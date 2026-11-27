// Command evil is a hostile peer for the p2p demo. Lab use only.
//
//	evil stall <sha256>   announces the pinned image, answers manifest and chunk map, never a chunk
//	evil lie   <sha256>   announces the image hash with a size the disk cannot hold (12 GiB)
//	evil fake  <sha256>   announces the true size and image hash, with chunk hashes of its own (zero) content
//	                      and serves them: only the manifest pin (?pin=) tells it from the real one
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/agent/pkg/p2p"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: evil stall|lie|fake <sha256>")
		os.Exit(2)
	}
	mode, pin := os.Args[1], os.Args[2]
	chunk := int64(4 << 20)
	size := int64(256 << 20)
	if mode == "lie" {
		size = 12 << 30
	}
	n := int((size + chunk - 1) / chunk)
	m := &p2p.Manifest{Version: 1, Image: "active", Size: size, ChunkSize: chunk, SHA256: pin}
	zero := make([]byte, chunk)
	zsum := sha256.Sum256(zero)
	for i := 0; i < n; i++ {
		if mode == "fake" {
			m.Chunks = append(m.Chunks, hex.EncodeToString(zsum[:]))
		} else {
			m.Chunks = append(m.Chunks, strings.Repeat("b", 64))
		}
	}
	have := p2p.NewBitmap(n)
	for i := 0; i < n; i++ {
		have.Set(i)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/active/manifest", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(m) })
	mux.HandleFunc("/active/have", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": m.ID(), "chunks": n, "have": have.Encode()})
	})
	mux.HandleFunc("/active/image", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[%s] chunk request %s\n", mode, r.Header.Get("Range"))
		if mode == "fake" {
			var a, b int64
			_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &b)
			if b >= size {
				b = size - 1
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, size))
			w.Header().Set("Content-Length", fmt.Sprint(b-a+1))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(zero[:b-a+1])
			return
		}
		select { // never answers
		case <-r.Context().Done():
		case <-time.After(time.Hour):
		}
	})
	ip, _ := net.InterfaceByName("eth0")
	addrs, _ := ip.Addrs()
	host := strings.Split(addrs[0].String(), "/")[0]
	l, err := net.Listen("tcp4", host+":42069")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("[%s] hostile peer on %s: announces image sha256 %s, %d bytes\n", mode, host, pin, size)
	_ = http.Serve(l, mux)
}
