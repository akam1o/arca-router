package netconf

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
)

const (
	// MaxChunkSize is the default maximum size of a single chunk in base:1.1 framing
	// Note: This is a local policy, not an RFC requirement. Some peers may send larger chunks.
	MaxChunkSize = 4096

	// MaxMessageSize is the maximum size of a complete NETCONF message
	MaxMessageSize = 16 * 1024 * 1024 // 16 MB

	// MaxChunkHeaderLength is the maximum length of a chunk header line (#<len>\n)
	// This prevents DoS attacks via unbounded header lines
	MaxChunkHeaderLength = 64 // Enough for "#" + 18 digits (max int64) + "\n"

	// EOMMarker is the end-of-message marker for base:1.0 framing
	EOMMarker = "]]>]]>"

	// ChunkEnd is the end-of-chunks marker for base:1.1 framing
	ChunkEnd = "##\n"
)

// FramingReader reads NETCONF messages using either base:1.0 (EOM) or base:1.1 (chunked) framing
type FramingReader struct {
	reader      *bufio.Reader
	baseVersion string // "1.0" or "1.1"
	buffer      bytes.Buffer
}

// NewFramingReader creates a new framing reader for the specified NETCONF base version
func NewFramingReader(r io.Reader, baseVersion string) *FramingReader {
	return &FramingReader{
		reader:      bufio.NewReader(r),
		baseVersion: baseVersion,
	}
}

// SetBaseVersion updates the base version without recreating the reader
// This preserves any buffered data when switching from base:1.0 to base:1.1
// after hello negotiation, which is critical for handling pipelined RPCs.
func (fr *FramingReader) SetBaseVersion(baseVersion string) {
	fr.baseVersion = baseVersion
}

// ReadMessage reads a complete NETCONF message using the appropriate framing protocol
func (fr *FramingReader) ReadMessage() ([]byte, error) {
	if fr.baseVersion == "1.1" {
		return fr.readChunkedMessage()
	}
	return fr.readEOMMessage()
}

// readBoundedLine reads a line up to maxLen bytes, preventing DoS via unbounded lines
func (fr *FramingReader) readBoundedLine(maxLen int) (string, error) {
	var line []byte
	for i := 0; i < maxLen; i++ {
		b, err := fr.reader.ReadByte()
		if err != nil {
			return "", err
		}
		line = append(line, b)
		if b == '\n' {
			return string(line), nil
		}
	}
	return "", fmt.Errorf("line exceeds maximum length %d", maxLen)
}

// readChunkedMessage reads a base:1.1 chunked message
// Format: #<len>\n<chunk>[#<len>\n<chunk>]...##\n
func (fr *FramingReader) readChunkedMessage() ([]byte, error) {
	fr.buffer.Reset()
	var totalSize int64 // Use int64 to prevent overflow

	for {
		// Read chunk header with bounded length (DoS prevention)
		line, err := fr.readBoundedLine(MaxChunkHeaderLength)
		if err != nil {
			return nil, fmt.Errorf("read chunk header: %w", err)
		}

		// Check for end-of-chunks marker
		if line == ChunkEnd {
			break
		}

		// Parse chunk length: #<len>\n
		if len(line) < 3 || line[0] != '#' || line[len(line)-1] != '\n' {
			return nil, fmt.Errorf("invalid chunk header format: %q", line)
		}

		chunkSizeStr := line[1 : len(line)-1]
		chunkSize, err := strconv.ParseInt(chunkSizeStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse chunk size: %w", err)
		}

		if chunkSize < 0 {
			return nil, fmt.Errorf("chunk size %d must be non-negative", chunkSize)
		}

		// Check individual chunk size limit (prevents single huge allocation)
		if chunkSize > int64(MaxMessageSize) {
			return nil, fmt.Errorf("chunk size %d exceeds message limit %d", chunkSize, MaxMessageSize)
		}

		// Check total message size limit with overflow protection
		totalSize += chunkSize
		if totalSize > int64(MaxMessageSize) {
			return nil, fmt.Errorf("message size %d exceeds limit %d", totalSize, MaxMessageSize)
		}

		// Read chunk data (safe cast after validation)
		chunkData := make([]byte, int(chunkSize))
		if _, err := io.ReadFull(fr.reader, chunkData); err != nil {
			return nil, fmt.Errorf("read chunk data: %w", err)
		}

		fr.buffer.Write(chunkData)
	}

	// Return a copy to prevent mutation if caller retains the slice
	result := make([]byte, fr.buffer.Len())
	copy(result, fr.buffer.Bytes())
	return result, nil
}

// readEOMMessage reads a base:1.0 EOM-delimited message
// Format: <message>]]>]]>
func (fr *FramingReader) readEOMMessage() ([]byte, error) {
	fr.buffer.Reset()
	totalSize := 0

	for {
		// Read byte by byte, looking for ]]>]]>
		b, err := fr.reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read byte: %w", err)
		}

		fr.buffer.WriteByte(b)
		totalSize++

		if totalSize > MaxMessageSize {
			return nil, fmt.Errorf("message size %d exceeds limit %d", totalSize, MaxMessageSize)
		}

		// Check for EOM marker
		if fr.buffer.Len() >= len(EOMMarker) {
			tail := fr.buffer.Bytes()[fr.buffer.Len()-len(EOMMarker):]
			if string(tail) == EOMMarker {
				// Remove EOM marker and return a copy
				dataLen := fr.buffer.Len() - len(EOMMarker)
				result := make([]byte, dataLen)
				copy(result, fr.buffer.Bytes()[:dataLen])
				return result, nil
			}
		}
	}
}

// FramingWriter writes NETCONF messages using either base:1.0 (EOM) or base:1.1 (chunked) framing
type FramingWriter struct {
	writer      io.Writer
	baseVersion string // "1.0" or "1.1"
}

// NewFramingWriter creates a new framing writer for the specified NETCONF base version
func NewFramingWriter(w io.Writer, baseVersion string) *FramingWriter {
	return &FramingWriter{
		writer:      w,
		baseVersion: baseVersion,
	}
}

// SetBaseVersion updates the base version without recreating the writer
// This allows switching framing modes after hello negotiation.
func (fw *FramingWriter) SetBaseVersion(baseVersion string) {
	fw.baseVersion = baseVersion
}

// WriteMessage writes a NETCONF message using the appropriate framing protocol
func (fw *FramingWriter) WriteMessage(data []byte) error {
	if fw.baseVersion == "1.1" {
		return fw.writeChunkedMessage(data)
	}
	return fw.writeEOMMessage(data)
}

// writeChunkedMessage writes a base:1.1 chunked message
// Format: #<len>\n<chunk>[#<len>\n<chunk>]...##\n
func (fw *FramingWriter) writeChunkedMessage(data []byte) error {
	remaining := len(data)
	offset := 0

	for remaining > 0 {
		chunkSize := remaining
		if chunkSize > MaxChunkSize {
			chunkSize = MaxChunkSize
		}

		// Write chunk header: #<len>\n
		header := fmt.Sprintf("#%d\n", chunkSize)
		if _, err := fw.writer.Write([]byte(header)); err != nil {
			return fmt.Errorf("write chunk header: %w", err)
		}

		// Write chunk data
		chunk := data[offset : offset+chunkSize]
		if _, err := fw.writer.Write(chunk); err != nil {
			return fmt.Errorf("write chunk data: %w", err)
		}

		offset += chunkSize
		remaining -= chunkSize
	}

	// Write end-of-chunks marker
	if _, err := fw.writer.Write([]byte(ChunkEnd)); err != nil {
		return fmt.Errorf("write chunk end: %w", err)
	}

	return nil
}

// writeEOMMessage writes a base:1.0 EOM-delimited message
// Format: <message>]]>]]>
func (fw *FramingWriter) writeEOMMessage(data []byte) error {
	// Validate that message doesn't contain EOM marker
	if bytes.Contains(data, []byte(EOMMarker)) {
		return fmt.Errorf("message contains EOM marker %q which would cause truncation in base:1.0", EOMMarker)
	}

	// Write message data
	if _, err := fw.writer.Write(data); err != nil {
		return fmt.Errorf("write message: %w", err)
	}

	// Write EOM marker
	if _, err := fw.writer.Write([]byte(EOMMarker)); err != nil {
		return fmt.Errorf("write EOM marker: %w", err)
	}

	return nil
}
