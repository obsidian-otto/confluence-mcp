package drawio

import (
	"bytes"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestWrapXmlToPng_ValidPngSignature asserts the output starts
// with the PNG magic bytes. The drawio format requires this
// because the file is read as a PNG by Confluence.
func TestWrapXmlToPng_ValidPngSignature(t *testing.T) {
	out := WrapXmlToPng([]byte(`<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/></root></mxGraphModel></diagram></mxfile>`))
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
	xml := `<mxfile><diagram name="hello"><mxGraphModel><root><mxCell id="1" value="hello"/></root></mxGraphModel></diagram></mxfile>`
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

// TestWrapXmlToPng_RoundTripsViaDrawioAlgorithm asserts the
// tEXt chunk's URL-decoded content is a valid mxfile XML
// containing the inner diagram content. This is what the
// drawio app's parser does on load.
func TestWrapXmlToPng_RoundTripsViaDrawioAlgorithm(t *testing.T) {
	inner := `<mxGraphModel dx="1200" dy="800" grid="1" gridSize="10" guides="1" tooltips="1" connect="1" arrows="1" fold="1" page="1" pageScale="1" pageWidth="1200" pageHeight="800" math="0" shadow="0"><root><mxCell id="0" /><mxCell id="1" parent="0" /></root></mxGraphModel>`
	full := `<mxfile><diagram name="hello">` + inner + `</diagram></mxfile>`

	pngBytes := WrapXmlToPng([]byte(full))

	// Find the tEXt chunk.
	pos := 8
	var textBytes []byte
	for pos+12 <= len(pngBytes) {
		length := int(pngBytes[pos])<<24 | int(pngBytes[pos+1])<<16 | int(pngBytes[pos+2])<<8 | int(pngBytes[pos+3])
		chunkType := string(pngBytes[pos+4 : pos+8])
		dataStart := pos + 8
		dataEnd := dataStart + length
		data := pngBytes[dataStart:dataEnd]
		if chunkType == "tEXt" {
			nullIdx := bytes.IndexByte(data, 0x00)
			if nullIdx >= 0 && string(data[:nullIdx]) == "mxfile" {
				textBytes = data[nullIdx+1:]
				break
			}
		}
		pos = dataEnd + 4
	}
	if textBytes == nil {
		t.Fatal("no mxfile tEXt chunk found")
	}

	// URL-decode (drawio app's first step).
	decoded, err := url.QueryUnescape(string(textBytes))
	if err != nil {
		t.Fatalf("QueryUnescape: %v", err)
	}

	// Must contain the mxfile root tag.
	if !strings.Contains(decoded, "<mxfile") {
		t.Errorf("URL-decoded text missing <mxfile>: %s", firstN(decoded, 200))
	}
	// Must contain the inner diagram content.
	if !strings.Contains(decoded, "mxGraphModel") {
		t.Errorf("URL-decoded text missing mxGraphModel: %s", firstN(decoded, 200))
	}
	// Must use %20 for spaces (drawio convention).
	if strings.Contains(string(textBytes), "+") {
		t.Errorf("tEXt text contains '+' (should be %%20): %s", firstN(string(textBytes), 200))
	}
}

// TestWrapXmlToPng_IHDRIsOneByOne asserts the IHDR chunk has
// width=1, height=1. The drawio format only requires that
// the PNG be valid — the image dimensions don't matter because
// the diagram content lives in the tEXt chunk, not the pixels.
func TestWrapXmlToPng_IHDRIsOneByOne(t *testing.T) {
	out := WrapXmlToPng([]byte(`<mxfile/>`))

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

// TestWrapXmlToPng_StripsOuterWrapper asserts that an input
// with an outer <mxfile> wrapper doesn't produce nested
// wrappers in the output.
func TestWrapXmlToPng_StripsOuterWrapper(t *testing.T) {
	withWrapper := `<mxfile><diagram><mxGraphModel><root><mxCell id="1"/></root></mxGraphModel></diagram></mxfile>`
	pngBytes := WrapXmlToPng([]byte(withWrapper))

	// Extract the URL-decoded tEXt text.
	pos := 8
	var textBytes []byte
	for pos+12 <= len(pngBytes) {
		length := int(pngBytes[pos])<<24 | int(pngBytes[pos+1])<<16 | int(pngBytes[pos+2])<<8 | int(pngBytes[pos+3])
		chunkType := string(pngBytes[pos+4 : pos+8])
		dataStart := pos + 8
		dataEnd := dataStart + length
		data := pngBytes[dataStart:dataEnd]
		if chunkType == "tEXt" {
			nullIdx := bytes.IndexByte(data, 0x00)
			if nullIdx >= 0 && string(data[:nullIdx]) == "mxfile" {
				textBytes = data[nullIdx+1:]
				break
			}
		}
		pos = dataEnd + 4
	}
	decoded, _ := url.QueryUnescape(string(textBytes))
	// Count <mxfile> openings. Should be exactly 1.
	if n := strings.Count(decoded, "<mxfile"); n != 1 {
		t.Errorf("decoded text has %d <mxfile> tags, want 1 (the wrapper, not nested): %s", n, firstN(decoded, 200))
	}
}

// TestWrapXmlToPng_AcceptsFragmentInput asserts that a
// fragment input (just the inner content, no outer wrapper)
// also works. The user might pass just an <mxGraphModel>
// fragment; the tool should still produce a valid drawio PNG.
func TestWrapXmlToPng_AcceptsFragmentInput(t *testing.T) {
	fragment := `<mxGraphModel><root><mxCell id="1" value="hello"/></root></mxGraphModel>`
	pngBytes := WrapXmlToPng([]byte(fragment))

	pos := 8
	var textBytes []byte
	for pos+12 <= len(pngBytes) {
		length := int(pngBytes[pos])<<24 | int(pngBytes[pos+1])<<16 | int(pngBytes[pos+2])<<8 | int(pngBytes[pos+3])
		chunkType := string(pngBytes[pos+4 : pos+8])
		dataStart := pos + 8
		dataEnd := dataStart + length
		data := pngBytes[dataStart:dataEnd]
		if chunkType == "tEXt" {
			nullIdx := bytes.IndexByte(data, 0x00)
			if nullIdx >= 0 && string(data[:nullIdx]) == "mxfile" {
				textBytes = data[nullIdx+1:]
				break
			}
		}
		pos = dataEnd + 4
	}
	decoded, _ := url.QueryUnescape(string(textBytes))
	if !strings.Contains(decoded, "mxCell") {
		t.Errorf("fragment input not preserved in output: %s", firstN(decoded, 200))
	}
}

// TestWrapToPng_ReadsFromDisk asserts the disk-reading variant
// matches the in-memory WrapXmlToPng.
func TestWrapToPng_ReadsFromDisk(t *testing.T) {
	xml := `<mxfile><diagram name="disk"><mxGraphModel><root><mxCell id="1"/></root></mxGraphModel></diagram></mxfile>`
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

// firstN returns up to n leading bytes of s as a string. Used in
// failure messages to keep the diff small.
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
