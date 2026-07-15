package wire

import (
	"fmt"
	"io"
)

// ReadString reads a Minecraft VarInt-length-prefixed UTF-8 string.
func ReadString(r io.ByteReader, maxChars int32) (string, error) {
	length, err := ReadVarInt(r)
	if err != nil {
		return "", err
	}
	if length < 0 || length > maxChars*4 {
		return "", fmt.Errorf("minecraft string byte length %d outside limit", length)
	}

	data := make([]byte, length)
	reader, ok := r.(io.Reader)
	if !ok {
		return "", fmt.Errorf("minecraft string reader does not implement io.Reader")
	}
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteString writes a Minecraft VarInt-length-prefixed UTF-8 string.
func WriteString(w io.ByteWriter, value string) error {
	if err := WriteVarInt(w, int32(len(value))); err != nil {
		return err
	}
	writer, ok := w.(io.Writer)
	if !ok {
		return fmt.Errorf("minecraft string writer does not implement io.Writer")
	}
	_, err := writer.Write([]byte(value))
	return err
}
