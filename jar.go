package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/rsc/zipmerge/zip"
	"os"
	"time"
)

// consts ...
const (
	ManifestPath = "META-INF/MANIFEST.MF"
	SFPath       = "META-INF/%s.SF"
	RSAPath      = "META-INF/%s.RSA"
	CPIDPath     = "cpid"
	LineWidth    = 70
)

func changeManifest(r *zip.Reader) error {
	buf, err := readManifest(r)
	if err != nil {
		return err
	}
	manifest := string(buf)

	// write MANIFEST.MF
	digest := sha1Sum([]byte(g.CPIDContent))

	cpidNameLine := fmt.Sprintf("Name: %s\r\n", CPIDPath)
	if cpidIndex := strings.Index(manifest, cpidNameLine); cpidIndex > 0 {
		// cpid file already exists
		beforePart := manifest[:cpidIndex]
		hashLineEnd := strings.Index(manifest[cpidIndex+len(cpidNameLine):], "\r\n")
		if hashLineEnd < 0 {
			return fmt.Errorf("malformed manifest: %s", manifest[cpidIndex:])
		}
		afterPart := manifest[cpidIndex+len(cpidNameLine)+hashLineEnd+2:]

		manifest = beforePart
		manifest += cpidNameLine
		manifest += fmt.Sprintf("SHA1-Digest: %s\r\n", digest)
		manifest += afterPart
	} else {
		// add cpid entry
		manifest += cpidNameLine
		manifest += fmt.Sprintf("SHA1-Digest: %s\r\n", digest)
		manifest += "\r\n"
	}

	err = ioutil.WriteFile(
		fmt.Sprintf("%s/MANIFEST.MF", g.WorkDir), []byte(manifest), 0644)
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
	mfDigest := sha1Sum([]byte(manifest))
	sf.WriteString(fmt.Sprintf("SHA1-Digest-Manifest: %s\r\n", mfDigest))
	sf.WriteString("\r\n")

	entries := strings.Split(manifest, "\r\n")
	for i := 0; i < len(entries); i++ {
		if strings.HasPrefix(entries[i], "Name: ") {
			nameLine := entries[i]
			i++
			if len(nameLine) >= LineWidth {
				if strings.HasPrefix(entries[i], " ") {
					nameLine += entries[i][1:]
					i++
				}
			}
			hashLine := entries[i]
			i++
			if len(hashLine) >= LineWidth {
				if strings.HasPrefix(entries[i], " ") {
					hashLine += entries[i][1:]
					i++
				}
			}
			msg := nameLine + "\r\n" + hashLine + "\r\n" + "\r\n"
			md := sha1Sum([]byte(msg))
			if len(nameLine) > LineWidth {
				sf.WriteString(nameLine[0:LineWidth] + "\r\n")
				sf.WriteString(" " + nameLine[70:] + "\r\n")
			} else {
				sf.WriteString(nameLine + "\r\n")
			}
			sf.WriteString(fmt.Sprintf("SHA1-Digest: %s\r\n", md))
			sf.WriteString("\r\n")
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

	header := &zip.FileHeader{
		Name:   to,
		Method: zip.Deflate,
	}
	header.SetModTime(time.Now())

	df, err := w.CreateHeader(header)
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
