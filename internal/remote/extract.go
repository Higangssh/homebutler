package remote

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
)

// releaseArchive names the release asset that carries the binary for one
// platform, and the binary's name inside it. Windows is published as a .zip
// holding homebutler.exe, every other platform as a .tar.gz holding
// homebutler: the names .goreleaser.yaml gives them.
func releaseArchive(osName, arch, version string) (archive, binary string) {
	if osName == "windows" {
		return fmt.Sprintf("homebutler_%s_%s_%s.zip", version, osName, arch), "homebutler.exe"
	}
	return fmt.Sprintf("homebutler_%s_%s_%s.tar.gz", version, osName, arch), "homebutler"
}

// extractBinary takes the binary out of a release archive named by
// releaseArchive.
func extractBinary(data []byte, archive, binary string) ([]byte, error) {
	if path.Ext(archive) == ".zip" {
		return extractBinaryFromZip(data, binary)
	}
	return extractBinaryFromTarGz(data)
}

// extractBinaryFromTarGz extracts the "homebutler" binary from a tar.gz archive.
func extractBinaryFromTarGz(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}

		// Look for the homebutler binary (might be at root or in a subdirectory)
		if hdr.Typeflag == tar.TypeReg && (hdr.Name == "homebutler" || bytes.HasSuffix([]byte(hdr.Name), []byte("/homebutler"))) {
			bin, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read binary: %w", err)
			}
			return bin, nil
		}
	}

	return nil, fmt.Errorf("homebutler binary not found in archive")
}

// extractBinaryFromZip extracts the named binary from a zip archive, at its
// root or in a directory, as extractBinaryFromTarGz does for a tar.gz.
func extractBinaryFromZip(data []byte, binary string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || path.Base(f.Name) != binary {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("zip: %w", err)
		}
		bin, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read binary: %w", err)
		}
		return bin, nil
	}
	return nil, fmt.Errorf("%s not found in archive", binary)
}
