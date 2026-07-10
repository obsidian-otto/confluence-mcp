// Package drawio — build a "drawio PNG" (a PNG with the drawio
// XML embedded in a tEXt chunk) so Confluence Cloud can render a
// drawio file as an inline diagram without requiring the drawio
// marketplace app to be installed.
//
// The wire format is:
//
//	PNG signature (8 bytes)
//	IHDR chunk
//	(optional other chunks: IDAT, etc.)
//	tEXt chunk with keyword "mxfile" and text = URL-encode(DEFLATE-compress(base64-decode(xml)))
//	(... more IDAT / IEND chunks)
//
// The drawio XML envelope is the standard one:
//
//	<mxfile><diagram name="..."> ... </diagram></mxfile>
//
// Confluence's stock (non-marketplace) renderer just shows the
// PNG. The drawio marketplace app, if installed, extracts the
// embedded XML and renders the editable diagram. Both paths
// work with this wrapper — see specs/12-drawio-attachments/
// 01-research-and-surface.md for the full rationale.
//
// The reverse direction (extract XML from a PNG) is NOT
// implemented here. md2conf has it (~80 LOC); we don't need
// it for the upload path. If a future tool needs it, copy
// hunyadi/md2conf/md2conf/drawio/render.py verbatim — the
// algorithm is non-trivial (PNG chunk parsing + raw DEFLATE
// with -zlib.MAX_WBITS).
package drawio

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"io"
	"net/url"
	"os"
)

// WrapToPng reads a standalone .drawio XML file from disk and
// returns a PNG byte stream with the XML embedded in a tEXt
// chunk. The PNG is the smallest valid 1x1 transparent PNG
// (the image data is a no-op — the embedded XML is what
// carries the diagram content; the PNG is just a vehicle
// for Confluence's renderer).
//
// The transformation is:
//
//	xml bytes
//	-> base64 encode (so the text is ASCII-safe inside the tEXt chunk)
//	-> DEFLATE compress (raw — -zlib.MAX_WBITS, no zlib/gzip headers)
//	-> URL-encode (the drawio format requires percent-encoding
//	   so the result survives any transport that re-parses the
//	   tEXt chunk)
//	-> embed in PNG tEXt chunk with keyword "mxfile"
//
// All three transforms match hunyadi/md2conf/md2conf/drawio/
// render.py:inflate + extract_xml_from_png, which is the
// canonical drawio PNG consumer. A file we produce can be
// read back by md2conf with zero information loss.
//
// Errors:
//   - file-not-found / not-a-regular-file / read-failure: wrapped
//     with the file path so the caller can surface a useful
//     message
//   - zlib / base64 / URL encode failures: cannot happen on
//     valid input (base64 is just b64.StdEncoding, URL is
//     QueryEscape which accepts any byte) but if they did,
//     we'd return an error rather than panic
func WrapToPng(drawioPath string) ([]byte, error) {
	xmlBytes, err := os.ReadFile(drawioPath)
	if err != nil {
		return nil, fmt.Errorf("drawio: read %q: %w", drawioPath, err)
	}
	return WrapXmlToPng(xmlBytes), nil
}

// WrapXmlToPng is the in-memory variant: takes the drawio XML
// bytes directly (useful for tests + for callers that already
// have the XML in memory). No I/O. Always succeeds on
// well-formed input.
func WrapXmlToPng(xmlBytes []byte) []byte {
	// Step 1: base64 encode the XML.
	b64 := base64.StdEncoding.EncodeToString(xmlBytes)

	// Step 2: raw DEFLATE compress (no zlib/gzip headers).
	// The drawio PNG spec uses -zlib.MAX_WBITS which is
	// exposed in Go as zlib's "raw" mode via
	// flate.NewWriter. See newRawDeflateWriter below.
	compressed := &bytes.Buffer{}
	fw, err := newRawDeflateWriter(compressed)
	if err != nil {
		// Cannot happen — newRawDeflateWriter only fails on
		// a nil io.Writer, which we never pass.
		panic(fmt.Sprintf("drawio: newRawDeflateWriter: %v", err))
	}
	if _, err := fw.Write([]byte(b64)); err != nil {
		panic(fmt.Sprintf("drawio: deflate write: %v", err))
	}
	if err := fw.Close(); err != nil {
		panic(fmt.Sprintf("drawio: deflate close: %v", err))
	}

	// Step 3: URL-encode the DEFLATE bytes. drawio's spec
	// requires percent-encoding so the result survives
	// re-parsing (the tEXt chunk's text field is itself
	// already text, but the bytes inside could be control
	// chars — URL-encoding makes them safe).
	urlEncoded := url.QueryEscape(compressed.String())
	// url.QueryEscape turns " " into "+" — drawio uses
	// %20. The QueryEscape result is good enough for
	// round-trip through url.QueryUnescape, but md2conf
	// uses unquote_to_bytes which only knows %XX. To be
	// safe with both decoders, swap "+" -> "%20".
	urlEncoded = string(bytes.ReplaceAll([]byte(urlEncoded), []byte("+"), []byte("%20")))

	// Step 4: build a minimal valid PNG with one IDAT chunk
	// containing a single transparent pixel. The pixel data
	// is irrelevant for drawio purposes — only the tEXt
	// chunk is read by the drawio renderer.
	return buildPngWithTextChunk("mxfile", string(urlEncoded))
}

// buildPngWithTextChunk constructs a minimal valid PNG with a
// single IDAT chunk (1x1 transparent pixel) plus one tEXt chunk
// carrying the supplied keyword + text. Used by WrapXmlToPng
// to bundle the encoded drawio XML.
//
// PNG layout per the spec (ISO/IEC 15948):
//
//	8-byte signature: 0x89 P N G \r \n 0x1A \n
//	IHDR chunk: width(4) height(4) bit-depth(1) color-type(1)
//	          compression(1) filter(1) interlace(1) = 13 bytes
//	IDAT chunk: zlib-compressed raw scanlines (we use the
//	          standard "1 transparent pixel" fixture)
//	tEXt chunk: keyword + 0x00 + text
//	IEND chunk: empty
//
// Each chunk is:
//
//	length(4 BE) | type(4) | data | crc32(4 BE)
//
// We compute CRC32 over (type || data) per the spec.
func buildPngWithTextChunk(keyword, text string) []byte {
	var buf bytes.Buffer

	// PNG signature.
	buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	// IHDR: 1x1, 8-bit, color type 6 (RGBA).
	ihdrData := []byte{
		0x00, 0x00, 0x00, 0x01, // width = 1
		0x00, 0x00, 0x00, 0x01, // height = 1
		0x08, // bit depth = 8
		0x06, // color type = 6 (RGBA)
		0x00, // compression = 0 (deflate)
		0x00, // filter = 0 (none)
		0x00, // interlace = 0 (no interlace)
	}
	writeChunk(&buf, "IHDR", ihdrData)

	// IDAT: one RGBA pixel (0,0,0,0) preceded by a filter
	// byte of 0. The 5 bytes are zlib-compressed.
	idatRaw := []byte{0x00, 0x00, 0x00, 0x00, 0x00} // filter=none + 4 zero bytes (transparent RGBA)
	idatData := zlibDeflate(idatRaw)
	writeChunk(&buf, "IDAT", idatData)

	// tEXt: keyword + 0x00 + text.
	textChunkData := make([]byte, 0, len(keyword)+1+len(text))
	textChunkData = append(textChunkData, keyword...)
	textChunkData = append(textChunkData, 0x00)
	textChunkData = append(textChunkData, text...)
	writeChunk(&buf, "tEXt", textChunkData)

	// IEND: empty.
	writeChunk(&buf, "IEND", nil)

	return buf.Bytes()
}

// writeChunk appends a single PNG chunk to buf in the layout
// length(4 BE) | type(4) | data | crc32(4 BE). The CRC is over
// (type || data) per the PNG spec.
func writeChunk(buf *bytes.Buffer, chunkType string, data []byte) {
	// Length prefix (4 bytes BE) — only counts the data,
	// not the type or the CRC.
	if err := buf.WriteByte(byte(len(data) >> 24)); err != nil {
		panic(err)
	}
	if err := buf.WriteByte(byte(len(data) >> 16)); err != nil {
		panic(err)
	}
	if err := buf.WriteByte(byte(len(data) >> 8)); err != nil {
		panic(err)
	}
	if err := buf.WriteByte(byte(len(data))); err != nil {
		panic(err)
	}

	// Type (4 bytes ASCII).
	buf.WriteString(chunkType)

	// Data.
	buf.Write(data)

	// CRC over type || data.
	c := crc32.NewIEEE()
	c.Write([]byte(chunkType))
	c.Write(data)
	crc := c.Sum32()

	if err := buf.WriteByte(byte(crc >> 24)); err != nil {
		panic(err)
	}
	if err := buf.WriteByte(byte(crc >> 16)); err != nil {
		panic(err)
	}
	if err := buf.WriteByte(byte(crc >> 8)); err != nil {
		panic(err)
	}
	if err := buf.WriteByte(byte(crc)); err != nil {
		panic(err)
	}
}

// zlibDeflate is a convenience wrapper over flate that produces
// a zlib-format stream (with the standard 2-byte header +
// Adler-32 checksum). Used for the IDAT chunk's compressed
// scanlines, where the PNG spec mandates zlib format (not raw
// DEFLATE). The idat path is the opposite of the tEXt path:
// PNG IDAT requires zlib-format, drawio's embedded XML
// requires raw DEFLATE.
func zlibDeflate(data []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// rawDeflateWriter is a tiny io.WriteCloser over compress/flate
// configured for raw DEFLATE output (no zlib header, no Adler-32
// checksum). Go's stdlib exposes this via flate.NewWriter with a
// negative or zero window size — but the simplest portable
// approach is to wrap the stdlib zlib.NewWriter and strip the
// 2-byte header + 4-byte trailer from the output.
//
// This is wasteful for big payloads but drawio diagrams are
// small (KB range, not MB) so the cost is negligible.
type rawDeflateWriter struct {
	buf *bytes.Buffer
	zw  io.WriteCloser // underlying zlib writer
}

// newRawDeflateWriter returns an io.WriteCloser that emits a raw
// DEFLATE stream (no zlib header, no Adler-32 trailer).
//
// Implementation: write to a zlib-format writer, then on Close
// strip the leading 2 bytes (CMF + FLG) and trailing 4 bytes
// (Adler-32 checksum) from the buffer.
//
// The drawio PNG spec is unambiguous about raw DEFLATE: the
// decompression side uses `-zlib.MAX_WBITS` (Python) which
// decodes raw DEFLATE only.
func newRawDeflateWriter(buf *bytes.Buffer) (io.WriteCloser, error) {
	zw := zlib.NewWriter(buf)
	return &rawDeflateWriter{buf: buf, zw: zw}, nil
}

func (w *rawDeflateWriter) Write(p []byte) (int, error) {
	return w.zw.Write(p)
}

func (w *rawDeflateWriter) Close() error {
	if err := w.zw.Close(); err != nil {
		return err
	}
	// Strip the zlib header (2 bytes) and trailer (4 bytes).
	// zlib header: CMF (1 byte) + FLG (1 byte) = 0x78 0x9C for
	// default compression. The 4-byte trailer is the
	// Adler-32 checksum of the uncompressed data.
	all := w.buf.Bytes()
	if len(all) < 6 {
		return fmt.Errorf("rawDeflateWriter: zlib output too short (%d bytes)", len(all))
	}
	raw := all[2 : len(all)-4]
	// Replace the buffer contents with the raw DEFLATE bytes.
	// We can't truncate a bytes.Buffer's underlying slice
	// directly, so rebuild it.
	w.buf.Reset()
	w.buf.Write(raw)
	return nil
}
