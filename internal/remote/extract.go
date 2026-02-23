package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

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
