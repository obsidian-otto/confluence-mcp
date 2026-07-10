package drawio

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestWrapXmlToPng_ValidPngSignature asserts the output starts
// with the PNG magic bytes. The drawio format requires this
// because the file is read as a PNG by Confluence.
func TestWrapXmlToPng_ValidPngSignature(t *testing.T) {
	out := WrapXmlToPng([]byte(`<mxfile><diagram name="x"/></mxfile>`))
	if len(out) < 8 {
		t.Fatalf("output too short: %d bytes", len(out))
	}
	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if !bytes.Equal(out[:8], pngSig) {
		t.Errorf("missing PNG signature; got %x", out[:8])
	}
}

// TestWrapXmlToPng_HasMxfileTextChunk asserts the file contains
// a tEXt chunk with keyword "mxfile". This is what drawio's
// parser looks for.
func TestWrapXmlToPng_HasMxfileTextChunk(t *testing.T) {
	xml := `<mxfile><diagram name="hello"/></mxfile>`
	out := WrapXmlToPng([]byte(xml))

	// Scan chunks after the signature.
	pos := 8
	found := false
	for pos+12 <= len(out) {
		length := int(out[pos])<<24 | int(out[pos+1])<<16 | int(out[pos+2])<<8 | int(out[pos+3])
		chunkType := string(out[pos+4 : pos+8])
		dataStart := pos + 8
		dataEnd := dataStart + length
		if dataEnd+4 > len(out) {
			t.Fatalf("truncated chunk %q at pos %d", chunkType, pos)
		}
		data := out[dataStart:dataEnd]

		if chunkType == "tEXt" {
			// tEXt format: keyword + 0x00 + text
			nullIdx := bytes.IndexByte(data, 0x00)
			if nullIdx < 0 {
				t.Errorf("tEXt chunk missing null separator")
			}
			keyword := string(data[:nullIdx])
			if keyword != "mxfile" {
				t.Errorf("tEXt keyword = %q, want 'mxfile'", keyword)
			}
			found = true
		}

		pos = dataEnd + 4
	}
	if !found {
		t.Error("no tEXt chunk found")
	}
}

// TestWrapXmlToPng_RoundTripsThroughDecoder asserts the file
// we produce can be decoded back to the original XML using the
// same algorithm md2conf uses. The decoder here is a Go port
// of hunyadi/md2conf/md2conf/drawio/render.py:decompress_diagram.
func TestWrapXmlToPng_RoundTripsThroughDecoder(t *testing.T) {
	original := `<mxfile><diagram name="round-trip"><mxGraphModel><root><mxCell id="1" value="hello"/></root></mxGraphModel></diagram></mxfile>`
	pngBytes := WrapXmlToPng([]byte(original))

	decoded, err := decodeMxfileTextFromPng(pngBytes)
	if err != nil {
		t.Fatalf("decodeMxfileTextFromPng: %v", err)
	}

	if decoded != original {
		t.Errorf("round-trip mismatch\n  got:  %q\n  want: %q", decoded, original)
	}
}

// TestWrapXmlToPng_IHDRIsOneByOne asserts the IHDR chunk has
// width=1, height=1. The drawio format only requires that
// the PNG be valid — the image dimensions don't matter because
// the diagram content lives in the tEXt chunk, not the pixels.
func TestWrapXmlToPng_IHDRIsOneByOne(t *testing.T) {
	out := WrapXmlToPng([]byte(`<mxfile/>`))

	// IHDR is the first chunk after the signature.
	pos := 8
	if got := string(out[pos+4 : pos+8]); got != "IHDR" {
		t.Fatalf("first chunk = %q, want IHDR", got)
	}
	width := int(out[pos+8])<<24 | int(out[pos+9])<<16 | int(out[pos+10])<<8 | int(out[pos+11])
	height := int(out[pos+12])<<24 | int(out[pos+13])<<16 | int(out[pos+14])<<8 | int(out[pos+15])
	if width != 1 || height != 1 {
		t.Errorf("IHDR = %dx%d, want 1x1", width, height)
	}
}

// TestWrapToPng_ReadsFromDisk asserts the disk-reading variant
// matches the in-memory WrapXmlToPng.
func TestWrapToPng_ReadsFromDisk(t *testing.T) {
	xml := `<mxfile><diagram name="disk"/></mxfile>`
	dir := t.TempDir()
	path := dir + "/test.drawio"
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	got, err := WrapToPng(path)
	if err != nil {
		t.Fatalf("WrapToPng: %v", err)
	}
	want := WrapXmlToPng([]byte(xml))
	if !bytes.Equal(got, want) {
		t.Errorf("WrapToPng != WrapXmlToPng (different bytes)")
	}
}

// TestWrapToPng_MissingFile asserts the error path is clean.
func TestWrapToPng_MissingFile(t *testing.T) {
	_, err := WrapToPng("/nonexistent/path/to/file.drawio")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("error %q does not mention read", err.Error())
	}
}

// decodeMxfileTextFromPng is the inverse of WrapXmlToPng —
// a Go port of md2conf's render.py decompression. Returns the
// original XML. The fact that WrapXmlToPng + this decoder form
// a lossless round-trip is the most important property of the
// wrapper: any PNG we produce must be readable by md2conf and
// by the drawio marketplace app.
func decodeMxfileTextFromPng(pngBytes []byte) (string, error) {
	if len(pngBytes) < 8 {
		return "", errShortInput
	}
	if !bytes.Equal(pngBytes[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "", errBadSignature
	}

	pos := 8
	for pos+12 <= len(pngBytes) {
		length := int(pngBytes[pos])<<24 | int(pngBytes[pos+1])<<16 | int(pngBytes[pos+2])<<8 | int(pngBytes[pos+3])
		chunkType := string(pngBytes[pos+4 : pos+8])
		dataStart := pos + 8
		dataEnd := dataStart + length
		if dataEnd+4 > len(pngBytes) {
			return "", errTruncated
		}
		data := pngBytes[dataStart:dataEnd]

		if chunkType == "tEXt" {
			nullIdx := bytes.IndexByte(data, 0x00)
			if nullIdx < 0 {
				continue
			}
			keyword := string(data[:nullIdx])
			if keyword != "mxfile" {
				continue
			}
			text := data[nullIdx+1:]

			// Inverse of urlEncoded: replace "+" back to " "
			// (in case some encoder used QueryEscape). Then
			// url.QueryUnescape to reverse percent-encoding.
			textStr := strings.ReplaceAll(string(text), "+", " ")
			unescaped, err := url.QueryUnescape(textStr)
			if err != nil {
				return "", err
			}

			// Inverse of raw DEFLATE compress: use flate
			// with negative MaxBits to get raw DEFLATE
			// decoding.
			decoded, err := rawInflate([]byte(unescaped))
			if err != nil {
				return "", err
			}

			// Inverse of base64 encode.
			xmlBytes, err := base64.StdEncoding.DecodeString(string(decoded))
			if err != nil {
				return "", err
			}
			return string(xmlBytes), nil
		}

		pos = dataEnd + 4
	}
	return "", errNoMxfile
}

// rawInflate decompresses raw DEFLATE (no zlib header/trailer)
// using compress/flate directly. Go's stdlib exposes raw DEFLATE
// inflate via flate.NewReader with a negative io.Reader that
// signals "no header" — the standard trick is to wrap the
// input in a custom Reader that prepends the bytes needed.
//
// The simplest portable approach: use compress/flate's
// Resetter + dictionary=""; but that requires a zlib header
// to be present. So we wrap: prepend a minimal zlib header
// (CMF=0x78, FLG=0x01 — no preset dictionary, default
// compression) and append a dummy Adler-32, inflate via
// zlib.NewReader, then drop the header and trailer from the
// output by knowing the input length.
//
// Actually the cleanest approach: use compress/flate directly
// with a negative window size via flate.NewReader. Looking at
// the stdlib docs: compress/flate.NewReader returns a
// io.ReadCloser that reads a raw DEFLATE stream when given
// one. (The negative-bits trick is on the Writer side.)
func rawInflate(data []byte) ([]byte, error) {
	// Wrap in zlib header (2 bytes) + Adler-32 trailer (4
	// bytes) so we can use zlib.NewReader. The Adler-32 of
	// the empty stream is 0x00000001, but Go's zlib reader
	// doesn't validate it strictly — passing a wrong
	// checksum produces a checksum-mismatch error which we
	// can ignore by using Discard.
	wrapped := append([]byte{0x78, 0x01}, data...)
	wrapped = append(wrapped, 0x00, 0x00, 0x00, 0x01) // dummy Adler-32
	r, err := zlib.NewReader(bytes.NewReader(wrapped))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		// zlib.Reader returns "checksum mismatch" because
		// our dummy Adler-32 is wrong. The decompressed
		// data is still valid — we got EOF before the
		// trailer was consumed. Just continue.
		if !strings.Contains(err.Error(), "checksum") {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

// Sentinel errors for the decoder.
var (
	errShortInput   = errString("drawio: input too short for PNG signature")
	errBadSignature = errString("drawio: missing PNG signature")
	errTruncated    = errString("drawio: truncated chunk in PNG")
	errNoMxfile     = errString("drawio: PNG contains no mxfile tEXt chunk")
)

type errString string

func (e errString) Error() string { return string(e) }
