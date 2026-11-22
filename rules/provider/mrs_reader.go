package provider

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

var MrsMagicBytes = [4]byte{'M', 'R', 'S', 1} // MRSv1

// mrsMaxDecodedSize bounds what one MRS file may decompress to, so a crafted file cannot
// expand without limit. Real sets stay far below it: MetaCubeX geosite-cn decodes to 0.9 MiB.
const mrsMaxDecodedSize = 256 << 20

// mrsDecode decompresses a whole MRS file. A streaming zstd reader allocates a history as large
// as the frame's window, 8 MiB for files from zstd's default encoder however small they are,
// while DecodeAll decodes straight into the output. The decoder is not shared: an idle one
// keeps its buffers and a reference to the last output it produced.
func mrsDecode(buf []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
		zstd.WithDecoderMaxMemory(mrsMaxDecodedSize),
	)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	return decoder.DecodeAll(buf, nil)
}

func rulesMrsParse(buf []byte, strategy ruleStrategy) (ruleStrategy, error) {
	if _strategy, ok := strategy.(mrsRuleStrategy); ok {
		decoded, err := mrsDecode(buf)
		if err != nil {
			return nil, err
		}
		reader := bytes.NewReader(decoded)

		// header
		var header [4]byte
		_, err = io.ReadFull(reader, header[:])
		if err != nil {
			return nil, err
		}
		if header != MrsMagicBytes {
			return nil, fmt.Errorf("invalid MrsMagic bytes")
		}

		// behavior
		var _behavior [1]byte
		_, err = io.ReadFull(reader, _behavior[:])
		if err != nil {
			return nil, err
		}
		if _behavior[0] != strategy.Behavior().Byte() {
			return nil, fmt.Errorf("invalid behavior")
		}

		// count
		var count int64
		err = binary.Read(reader, binary.BigEndian, &count)
		if err != nil {
			return nil, err
		}

		// extra (reserved for future using)
		var length int64
		err = binary.Read(reader, binary.BigEndian, &length)
		if err != nil {
			return nil, err
		}
		if length < 0 || length > int64(reader.Len()) {
			return nil, errors.New("length is invalid")
		}
		if length > 0 {
			extra := make([]byte, length)
			_, err = io.ReadFull(reader, extra)
			if err != nil {
				return nil, err
			}
		}

		err = _strategy.FromMrs(reader, int(count))
		return strategy, err
	} else {
		return nil, ErrInvalidFormat
	}
}
