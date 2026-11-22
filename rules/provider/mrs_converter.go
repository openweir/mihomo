package provider

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	P "github.com/metacubex/mihomo/constant/provider"

	"github.com/klauspost/compress/zstd"
)

func ConvertToMrs(buf []byte, behavior P.RuleBehavior, format P.RuleFormat, w io.Writer) (err error) {
	strategy := newStrategy(behavior, nil)
	strategy, err = rulesParse(buf, strategy, format)
	if err != nil {
		return err
	}
	if strategy.Count() == 0 {
		return errors.New("empty rule")
	}
	if _strategy, ok := strategy.(mrsRuleStrategy); ok {
		if format == P.MrsRule { // export to TextRule
			_strategy.DumpMrs(func(key string) bool {
				_, err = fmt.Fprintln(w, key)
				if err != nil {
					return false
				}
				return true
			})
			return nil
		}

		// Compressed in one piece below, so that the frame records the payload's size and a window
		// no larger than it. zstd's streaming encoder declares an 8 MiB window, which streaming
		// readers (older cores among them) allocate in full even for a small set.
		payload := new(bytes.Buffer)

		// header
		_, err = payload.Write(MrsMagicBytes[:])
		if err != nil {
			return err
		}

		// behavior
		_behavior := []byte{behavior.Byte()}
		_, err = payload.Write(_behavior[:])
		if err != nil {
			return err
		}

		// count
		count := int64(_strategy.Count())
		err = binary.Write(payload, binary.BigEndian, count)
		if err != nil {
			return err
		}

		// extra (reserved for future using)
		var extra []byte
		err = binary.Write(payload, binary.BigEndian, int64(len(extra)))
		if err != nil {
			return err
		}
		_, err = payload.Write(extra)
		if err != nil {
			return err
		}

		err = _strategy.WriteMrs(payload)
		if err != nil {
			return err
		}

		var encoder *zstd.Encoder
		encoder, err = zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
		if err != nil {
			return err
		}
		defer encoder.Close()
		_, err = w.Write(encoder.EncodeAll(payload.Bytes(), nil))
		return err
	} else {
		return ErrInvalidFormat
	}
}

func ConvertMain(args []string) {
	if len(args) > 3 {
		behavior, err := P.ParseBehavior(args[0])
		if err != nil {
			panic(err)
		}
		format, err := P.ParseRuleFormat(args[1])
		if err != nil {
			panic(err)
		}
		source := args[2]
		target := args[3]

		sourceFile, err := os.ReadFile(source)
		if err != nil {
			panic(err)
		}

		targetFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			panic(err)
		}

		err = ConvertToMrs(sourceFile, behavior, format, targetFile)
		if err != nil {
			panic(err)
		}

		err = targetFile.Close()
		if err != nil {
			panic(err)
		}
	} else {
		panic("Usage: convert-ruleset <behavior> <format> <source file> <target file>")
	}
}
