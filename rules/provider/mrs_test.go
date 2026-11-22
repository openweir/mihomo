package provider

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/netip"
	"runtime"
	"strings"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MRS files written by zstd's default streaming encoder (MetaCubeX's sets among them)
// declare an 8 MiB window whatever their size.
const defaultEncoderWindow = 8 << 20

// spacedCIDRs returns n /24 ranges with a gap after each, so the set keeps n ranges.
func spacedCIDRs(n int) []string {
	rules := make([]string, n)
	for i := range rules {
		rules[i] = fmt.Sprintf("10.%d.%d.0/24", i/128, i%128*2)
	}
	return rules
}

func manyDomains(n int) []string {
	rules := make([]string, n)
	for i := range rules {
		rules[i] = fmt.Sprintf("+.host%d.zone%d.example", i, i%97)
	}
	return rules
}

func convertToMrs(t testing.TB, behavior P.RuleBehavior, format P.RuleFormat, buf []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, ConvertToMrs(buf, behavior, format, &out))
	return out.Bytes()
}

func decompress(t testing.TB, frame []byte) []byte {
	t.Helper()
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer decoder.Close()
	payload, err := decoder.DecodeAll(frame, nil)
	require.NoError(t, err)
	return payload
}

func compress(t testing.TB, payload []byte) []byte {
	t.Helper()
	encoder, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	defer encoder.Close()
	return encoder.EncodeAll(payload, nil)
}

// compressStream compresses payload the way ConvertToMrs used to: with zstd's default
// streaming encoder, which writes no content size and an 8 MiB window once the payload
// spans more than one block.
func compressStream(t testing.TB, payload []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	encoder, err := zstd.NewWriter(&out)
	require.NoError(t, err)
	_, err = encoder.Write(payload)
	require.NoError(t, err)
	require.NoError(t, encoder.Close())
	return out.Bytes()
}

func frameWindow(t testing.TB, frame []byte) (window uint64, header zstd.Header) {
	t.Helper()
	require.NoError(t, header.Decode(frame))
	if header.SingleSegment {
		return header.FrameContentSize, header
	}
	return header.WindowSize, header
}

func parseMrs(behavior P.RuleBehavior, mrs []byte) (ruleStrategy, error) {
	return rulesParse(mrs, newStrategy(behavior, nil), P.MrsRule)
}

func dump(strategy ruleStrategy) []string {
	var keys []string
	strategy.(mrsRuleStrategy).DumpMrs(func(key string) bool {
		keys = append(keys, key)
		return true
	})
	return keys
}

// allocated reports the bytes f allocates, averaged over runs.
func allocated(runs int, f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range runs {
		f()
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(runs)
}

func TestMrsRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		behavior P.RuleBehavior
		rules    []string
		hit      []string
		miss     []string
	}{
		{
			name:     "domain",
			behavior: P.Domain,
			rules:    []string{"+.example.com", "example.net", ".example.org", "*.wild.example"},
			hit:      []string{"example.com", "www.example.com", "example.net", "www.example.org", "a.wild.example"},
			miss:     []string{"example.org", "www.example.net", "wild.example", "example.edu"},
		},
		{
			name:     "domain-large",
			behavior: P.Domain,
			rules:    manyDomains(20000),
			hit:      []string{"host0.zone0.example", "a.host19999.zone17.example"},
			miss:     []string{"host20000.zone17.example", "zone0.example"},
		},
		{
			name:     "ipcidr",
			behavior: P.IPCIDR,
			rules:    []string{"192.0.2.0/24", "198.51.100.7/32", "2001:db8::/32"},
			hit:      []string{"192.0.2.1", "198.51.100.7", "2001:db8::1"},
			miss:     []string{"192.0.3.1", "198.51.100.8", "2001:db9::1"},
		},
		{
			name:     "ipcidr-large",
			behavior: P.IPCIDR,
			rules:    spacedCIDRs(6000),
			hit:      []string{"10.0.0.1", "10.46.222.255"},
			miss:     []string{"10.0.1.1", "10.47.0.1"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			text := []byte(strings.Join(test.rules, "\n"))
			want, err := rulesParse(text, newStrategy(test.behavior, nil), P.TextRule)
			require.NoError(t, err)

			mrs := convertToMrs(t, test.behavior, P.TextRule, text)
			payload := decompress(t, mrs)

			// Older cores read MRS files with a streaming decoder.
			stream, err := zstd.NewReader(bytes.NewReader(mrs))
			require.NoError(t, err)
			streamed, err := io.ReadAll(stream)
			stream.Close()
			require.NoError(t, err)
			assert.Equal(t, payload, streamed)

			got, err := parseMrs(test.behavior, mrs)
			require.NoError(t, err)
			assert.Equal(t, want.Count(), got.Count())
			assert.Equal(t, dump(want), dump(got))
			for _, rule := range test.hit {
				assert.True(t, got.Match(metadataFor(test.behavior, rule), C.RuleMatchHelper{}), rule)
			}
			for _, rule := range test.miss {
				assert.False(t, got.Match(metadataFor(test.behavior, rule), C.RuleMatchHelper{}), rule)
			}

			// Files written by the streaming encoder, as MetaCubeX's are, read the same.
			legacy, err := parseMrs(test.behavior, compressStream(t, payload))
			require.NoError(t, err)
			assert.Equal(t, dump(want), dump(legacy))

			// Exporting an MRS file back to text yields the rules it holds.
			exported := convertToMrs(t, test.behavior, P.MrsRule, mrs)
			assert.Equal(t, strings.Join(dump(want), "\n")+"\n", string(exported))
		})
	}
}

func metadataFor(behavior P.RuleBehavior, rule string) *C.Metadata {
	if behavior == P.IPCIDR {
		return &C.Metadata{DstIP: netip.MustParseAddr(rule)}
	}
	return &C.Metadata{Host: rule}
}

// A small set compressed with an 8 MiB window used to cost a window-sized history buffer
// (about 9 MiB) to read.
func TestMrsParseLargeWindowAllocation(t *testing.T) {
	tests := []struct {
		name     string
		behavior P.RuleBehavior
		rules    []string
	}{
		{"domain", P.Domain, manyDomains(20000)},
		{"ipcidr", P.IPCIDR, spacedCIDRs(6000)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := decompress(t, convertToMrs(t, test.behavior, P.TextRule, []byte(strings.Join(test.rules, "\n"))))
			mrs := compressStream(t, payload)
			window, header := frameWindow(t, mrs)
			require.EqualValues(t, defaultEncoderWindow, window)
			require.False(t, header.HasFCS)

			strategy, err := parseMrs(test.behavior, mrs)
			require.NoError(t, err)
			require.Equal(t, len(test.rules), strategy.Count())

			perParse := allocated(5, func() {
				_, err = parseMrs(test.behavior, mrs)
			})
			require.NoError(t, err)
			t.Logf("%d compressed, %d decompressed bytes: %d bytes allocated per parse", len(mrs), len(payload), perParse)
			assert.Less(t, perParse, uint64(4<<20))
		})
	}
}

func mrsHeader(behavior P.RuleBehavior, count, extraLength int64) []byte {
	header := append(MrsMagicBytes[:], behavior.Byte())
	header = binary.BigEndian.AppendUint64(header, uint64(count))
	return binary.BigEndian.AppendUint64(header, uint64(extraLength))
}

func TestMrsParseRejectsMalformed(t *testing.T) {
	text := []byte(strings.Join([]string{"+.example.com", "example.net"}, "\n"))
	mrs := convertToMrs(t, P.Domain, P.TextRule, text)
	payload := decompress(t, mrs)
	body := payload[len(mrsHeader(P.Domain, 0, 0)):]

	// A zstd frame that claims a terabyte of content.
	hugeFrame := []byte{0x28, 0xb5, 0x2f, 0xfd, 0xe0} // magic; single segment, 8-byte content size
	hugeFrame = binary.LittleEndian.AppendUint64(hugeFrame, 1<<40)
	hugeFrame = append(hugeFrame, 0x01, 0x00, 0x00) // last raw block, empty

	tests := []struct {
		name     string
		behavior P.RuleBehavior
		mrs      []byte
	}{
		{"empty", P.Domain, nil},
		{"not zstd", P.Domain, payload},
		{"truncated frame", P.Domain, mrs[:len(mrs)/2]},
		{"corrupt frame", P.Domain, append(append([]byte{}, mrs[:len(mrs)-4]...), 0xde, 0xad, 0xbe, 0xef)},
		{"huge frame", P.Domain, hugeFrame},
		{"bad magic", P.Domain, compress(t, append([]byte("MRS\x02"), payload[4:]...))},
		{"wrong behavior", P.IPCIDR, mrs},
		{"short header", P.Domain, compress(t, payload[:12])},
		{"truncated body", P.Domain, compress(t, payload[:len(payload)-1])},
		{"negative extra", P.Domain, compress(t, append(mrsHeader(P.Domain, 2, -1), body...))},
		{"extra past end", P.Domain, compress(t, append(mrsHeader(P.Domain, 2, int64(len(body)+1)), body...))},
		{"huge extra", P.Domain, compress(t, append(mrsHeader(P.Domain, 2, 1<<40), body...))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var err error
			allocatedBytes := allocated(1, func() {
				_, err = parseMrs(test.behavior, test.mrs)
			})
			assert.Error(t, err)
			assert.Less(t, allocatedBytes, uint64(4<<20))
		})
	}

	// The valid file still parses.
	strategy, err := parseMrs(P.Domain, compress(t, append(mrsHeader(P.Domain, 2, 3), append([]byte{1, 2, 3}, body...)...)))
	require.NoError(t, err)
	assert.Equal(t, 2, strategy.Count())
}
