//go:build acceptance

package harness

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/TomazVidovic533/stackpuddle/artifacts"
)

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Server serves the bundle and module archives on 127.0.0.1 only. While Corrupt is set it serves each module
// archive with one byte flipped, so the bytes no longer match the digest the lock names.
type Server struct {
	URL     string
	Corrupt atomic.Bool
	http    *httptest.Server
}

func (s *Stack) archive(m Module) ([]byte, string) {
	t := s.t
	t.Helper()
	src, ok := s.cfg.Archives[m.Name]
	if !ok {
		src = ArchiveSource{Dir: filepath.Join(s.cfg.Fixture, m.Source)}
	}
	if src.File != "" {
		data, err := os.ReadFile(src.File)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := artifacts.Digest(bytes.NewReader(data))
		if err != nil || digest != src.Digest {
			t.Fatalf("harness: archive %s has digest %q (%v), want %q", src.File, digest, err, src.Digest)
		}
		return data, digest
	}
	var buf bytes.Buffer
	digest, err := artifacts.Build(artifacts.Module, src.Dir, &buf)
	if err != nil {
		t.Fatalf("harness: build module archive from %s: %v", src.Dir, err)
	}
	return buf.Bytes(), digest
}

// serve builds the archives, starts the server and sets s.lock.
func (s *Stack) serve() {
	t := s.t
	t.Helper()
	for range s.Fixture.Environments {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		s.ports = append(s.ports, l.Addr().(*net.TCPAddr).Port)
		_ = l.Close()
	}
	bundleDir := t.TempDir()
	version := s.Fixture.Modules[0].Version
	bundle := "apiVersion: stackpuddle/v1\nkind: Bundle\nversion: " + version + "\nmodules:\n"
	for _, m := range s.Fixture.Modules {
		bundle += fmt.Sprintf("  %s:\n    version: %s\n", m.Name, m.Version)
	}
	if err := writeFile(filepath.Join(bundleDir, "bundle.yaml"), bundle); err != nil {
		t.Fatal(err)
	}
	var bundleBytes bytes.Buffer
	bundleDigest, err := artifacts.Build(artifacts.Bundle, bundleDir, &bundleBytes)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/bundle.tar.gz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bundleBytes.Bytes()) })
	type served struct {
		m      Module
		data   []byte
		digest string
	}
	var all []served
	for _, m := range s.Fixture.Modules {
		data, digest := s.archive(m)
		all = append(all, served{m, data, digest})
		mux.HandleFunc("/"+m.Name+".tar.gz", func(w http.ResponseWriter, _ *http.Request) {
			out := data
			if srv.Corrupt.Load() {
				out = append([]byte(nil), data...)
				out[len(out)/2] ^= 0xff
			}
			_, _ = w.Write(out)
		})
	}
	srv.http = httptest.NewServer(mux) // listens on 127.0.0.1 only
	srv.URL = srv.http.URL
	t.Cleanup(srv.http.Close)
	s.Server = srv
	lock := fmt.Sprintf("apiVersion: stackpuddle/v1\nkind: Lock\nbundle:\n  version: %s\n  source: %s/bundle.tar.gz\n  digest: %s\nmodules:\n", version, srv.URL, bundleDigest)
	for _, a := range all {
		lock += fmt.Sprintf("  %s:\n    version: %s\n    source: %s/%s.tar.gz\n    format: tar+gzip\n    digest: %s\n", a.m.Name, a.m.Version, srv.URL, a.m.Name, a.digest)
	}
	s.lock = lock
}
