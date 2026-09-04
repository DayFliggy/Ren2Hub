package service

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func makeHEIFBox(boxType string, content []byte) []byte {
	box := make([]byte, 8+len(content))
	binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
	copy(box[4:8], boxType)
	copy(box[8:], content)
	return box
}

func makeHEIFDocument(nestedDepth int) []byte {
	ispeContent := make([]byte, 12)
	binary.BigEndian.PutUint32(ispeContent[4:8], 640)
	binary.BigEndian.PutUint32(ispeContent[8:12], 480)
	nested := makeHEIFBox("ispe", ispeContent)
	for i := 0; i < nestedDepth; i++ {
		nested = makeHEIFBox("iprp", nested)
	}

	metaContent := append([]byte{0, 0, 0, 0}, nested...)
	meta := makeHEIFBox("meta", metaContent)
	ftyp := make([]byte, 16)
	binary.BigEndian.PutUint32(ftyp[:4], uint32(len(ftyp)))
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "heic")
	return append(ftyp, meta...)
}

func TestParseHEIFDimensions(t *testing.T) {
	config, format, err := decodeImageConfig(makeHEIFDocument(2))
	require.NoError(t, err)
	require.Equal(t, "heic", format)
	require.Equal(t, 640, config.Width)
	require.Equal(t, 480, config.Height)
}

func TestParseHEIFDimensionsRejectsExcessiveNesting(t *testing.T) {
	config, format, err := decodeImageConfig(
		makeHEIFDocument(maxHEIFBoxDepth + 1),
	)
	require.Error(t, err)
	require.Empty(t, format)
	require.Zero(t, config.Width)
	require.Zero(t, config.Height)
}
