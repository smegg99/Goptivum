//go:build !no_optivum

// core/optivum/pla/codec.go

package pla

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// Available reports whether this build carries the Optivum codec.
const Available = true

// Decode unwraps a container into its plan XML.
func Decode(data []byte) (File, error) {
	if len(data) < 2 {
		return File{}, ErrTruncated
	}
	magicLen := int(binary.LittleEndian.Uint16(data[:2]))
	if len(data) < 2+magicLen {
		return File{}, ErrTruncated
	}
	file := File{Magic: string(data[2 : 2+magicLen])}
	if !strings.HasPrefix(file.Magic, magicPrefix) {
		return File{}, fmt.Errorf("%w: magic %q", ErrUnknownFormat, file.Magic)
	}

	payload := make([]byte, len(data)-2-magicLen)
	copy(payload, data[2+magicLen:])
	mask(payload)

	if !looksZlib(payload) {
		file.XML = payload
		return file, nil
	}
	reader, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		return File{}, fmt.Errorf("pla: open zlib payload: %w", err)
	}
	defer reader.Close()
	xml, err := io.ReadAll(reader)
	if err != nil {
		return File{}, fmt.Errorf("pla: inflate payload: %w", err)
	}
	file.XML = xml
	file.Compressed = true
	return file, nil
}

// Encode wraps plan XML back into a container. An empty Magic defaults to the
// generation this codec writes.
//
// The bytes are not identical to the ones Optivum itself would emit: deflate
// output differs between implementations. The result is a valid container
// carrying identical XML, which is what readers of the format consume.
func Encode(file File) ([]byte, error) {
	magic := file.Magic
	if magic == "" {
		magic = Magic
	}
	if len(magic) > 0xFFFF {
		return nil, ErrMagicTooLong
	}
	if !strings.HasPrefix(magic, magicPrefix) {
		return nil, fmt.Errorf("%w: magic %q", ErrUnknownFormat, magic)
	}

	payload := file.XML
	if file.Compressed {
		var buffer bytes.Buffer
		// Level 9: what the observed files carry (zlib header 0x78DA).
		writer, err := zlib.NewWriterLevel(&buffer, zlib.BestCompression)
		if err != nil {
			return nil, fmt.Errorf("pla: open zlib writer: %w", err)
		}
		if _, err := writer.Write(file.XML); err != nil {
			return nil, fmt.Errorf("pla: compress payload: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("pla: finish payload: %w", err)
		}
		payload = buffer.Bytes()
	}

	out := make([]byte, 2+len(magic)+len(payload))
	binary.LittleEndian.PutUint16(out[:2], uint16(len(magic)))
	copy(out[2:], magic)
	copy(out[2+len(magic):], payload)
	mask(out[2+len(magic):])
	return out, nil
}
