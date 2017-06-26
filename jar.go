package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/rsc/zipmerge/zip"
	"os"
)

// consts ...
const (
	ManifestPath = "META-INF/MANIFEST.MF"
	SFPath       = "META-INF/%s.SF"
	RSAPath      = "META-INF/%s.RSA"
	CPIDPath     = "cpid"
)

func changeManifest(r *zip.Reader) error {
	manifest, err := readManifest(r)
	if err != nil {
		return err
	}

	// write MANIFEST.MF
	digest := sha256Sum([]byte(g.CPIDContent))
	manifest = append(manifest, []byte("Name: cpid\r\n")...)
	manifest = append(
		manifest,
		[]byte(fmt.Sprintf("SHA-256-Digest: %s\r\n", digest))...)
	manifest = append(manifest, []byte("\r\n")...)

	err = ioutil.WriteFile(
		fmt.Sprintf("%s/MANIFEST.MF", g.WorkDir), manifest, 0644)
	if err != nil {
		return err
	}

	// write CERT.SF
	sf, err := os.Create(fmt.Sprintf("%s/%s.SF", g.WorkDir, g.CertName))
	if err != nil {
		return err
	}
	defer sf.Close()

	sf.WriteString("Signature-Version: 1.0\r\n")
	mfDigest := sha256Sum(manifest)
	sf.WriteString(fmt.Sprintf("SHA-256-Digest-Manifest: %s\r\n", mfDigest))
	sf.WriteString("\r\n")

	entries := strings.Split(string(manifest), "\r\n")
	for i := 0; i < len(entries); i++ {
		if strings.HasPrefix(entries[i], "Name: ") {
			msg := entries[i] + "\r\n"
			msg += entries[i+1] + "\r\n"
			msg += "\r\n"
			md := sha256Sum([]byte(msg))
			sf.WriteString(entries[i] + "\r\n")
			sf.WriteString(fmt.Sprintf("SHA-256-Digest: %s\r\n", md))
			sf.WriteString("\r\n")
			i++
		}
	}

	// write CERT.RSA
	rsa, err := signSF()
	if err != nil {
		return err
	}

	return ioutil.WriteFile(
		fmt.Sprintf("%s/%s.RSA", g.WorkDir, g.CertName), rsa, 0644)
}

func readManifest(r *zip.Reader) ([]byte, error) {
	for _, f := range r.File {
		if f.Name == ManifestPath {
			fr, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer fr.Close()
			buf, err := ioutil.ReadAll(fr)
			if err != nil {
				return nil, err
			}
			return buf, nil
		}
	}

	return nil, fmt.Errorf("manifest file not found")
}

// copyFile ...
func copyFile(w *zip.Writer, to, src string) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	df, err := w.Create(to)
	if err != nil {
		return err
	}

	_, err = io.Copy(df, sf)
	return err
}

// copyContent ...
func copyContent(w *zip.Writer, to, content string) error {
	df, err := w.Create(to)
	if err != nil {
		return err
	}

	n, err := df.Write([]byte(content))
	if n != len(content) {
		return fmt.Errorf("expect write %d bytes, actual: %d", len(content), n)
	}
	return err
}

// copyCPID ...
func copyCPID(w *zip.Writer) error {
	return copyContent(w, CPIDPath, g.CPIDContent)
}

// copyMeta ...
func copyMeta(w *zip.Writer) error {
	// MANIFEST.MF
	source := fmt.Sprintf("%s/MANIFEST.MF", g.WorkDir)
	dest := ManifestPath
	if err := copyFile(w, dest, source); err != nil {
		return err
	}
	// CERT.SF
	source = fmt.Sprintf("%s/%s.SF", g.WorkDir, g.CertName)
	dest = fmt.Sprintf(SFPath, g.CertName)
	if err := copyFile(w, dest, source); err != nil {
		return err
	}

	// CERT.RSA
	source = fmt.Sprintf("%s/%s.RSA", g.WorkDir, g.CertName)
	dest = fmt.Sprintf(RSAPath, g.CertName)
	if err := copyFile(w, dest, source); err != nil {
		return err
	}

	return nil
}
