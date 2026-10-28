package tls_test

import (
	"bytes"
	gotls "crypto/tls"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/common/protocol/tls/cert"
	. "github.com/xtls/xray-core/transport/internet/tls"
)

func TestCertificateReloadConcurrentSelection(t *testing.T) {
	first, _ := cert.MustGenerate(nil, cert.CommonName("first"))
	second, _ := cert.MustGenerate(nil, cert.CommonName("second"))
	entry := ParseCertificate(first)
	folder := t.TempDir()
	entry.CertificatePath = filepath.Join(folder, "cert.pem")
	entry.KeyPath = filepath.Join(folder, "key.pem")
	entry.OcspStapling = 1 // self-signed local fixtures have no OCSP URL; no network fetch
	secondCert, secondKey := second.ToPEM()
	if err := os.WriteFile(entry.CertificatePath, entry.Certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry.KeyPath, entry.Key, 0o600); err != nil {
		t.Fatal(err)
	}
	config := (&Config{Certificate: []*Certificate{entry}}).GetTLSConfig()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				selected, err := config.GetCertificate(&gotls.ClientHelloInfo{ServerName: "local"})
				if err != nil || selected == nil || selected.Leaf == nil {
					t.Error("invalid certificate selection", err)
					return
				}
				// Retain/read the certificate after GetCertificate releases its selector lock.
				_ = len(selected.OCSPStaple)
				time.Sleep(time.Millisecond)
			}
		})
	}
	defer func() { close(stop); wg.Wait() }()
	// Rotate only after selectors are running; exercise a later ticker publication.
	if err := os.WriteFile(entry.CertificatePath, secondCert, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry.KeyPath, secondKey, 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		selected, err := config.GetCertificate(&gotls.ClientHelloInfo{})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(selected.Certificate[0], second.Certificate) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("replacement certificate never reached the live selector")
}
