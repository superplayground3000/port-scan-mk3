package state

import (
	"bufio"
	"encoding/json"
	"io"
	"strconv"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type snapshotEncoder struct {
	writer *bufio.Writer
}

func encodeSnapshot(writer *bufio.Writer, env snapshotEnvelope) error {
	encoder := snapshotEncoder{writer: writer}
	if err := encoder.text("{\n  \"chunks\": "); err != nil {
		return err
	}
	if err := encoder.chunks(env.Chunks); err != nil {
		return err
	}
	if env.PreScanPing != nil {
		if err := encoder.preScanPing(env.PreScanPing); err != nil {
			return err
		}
	}
	if env.Output != nil {
		if err := encoder.output(env.Output); err != nil {
			return err
		}
	}
	if env.RichDenyExcluded {
		if err := encoder.text(",\n  \"rich_deny_excluded\": true"); err != nil {
			return err
		}
	}
	if env.TargetExpansion != nil {
		if err := encoder.targetExpansion(env.TargetExpansion); err != nil {
			return err
		}
	}
	if env.TargetSemanticsVersion != 0 {
		if err := encoder.text(",\n  \"target_semantics_version\": " + strconv.Itoa(env.TargetSemanticsVersion)); err != nil {
			return err
		}
	}
	if len(env.BasicPortFallback) > 0 {
		if err := encoder.stringArray(",\n  \"basic_port_fallback\": ", env.BasicPortFallback, "    "); err != nil {
			return err
		}
	}
	return encoder.text("\n}")
}

func (encoder snapshotEncoder) chunks(chunks *[]task.Chunk) error {
	if chunks == nil || *chunks == nil {
		return encoder.text("null")
	}
	if err := encoder.text("["); err != nil {
		return err
	}
	for index := range *chunks {
		if index > 0 {
			if err := encoder.text(","); err != nil {
				return err
			}
		}
		if err := encoder.chunk((*chunks)[index]); err != nil {
			return err
		}
	}
	if len(*chunks) > 0 {
		return encoder.text("\n  ]")
	}
	return encoder.text("]")
}

func (encoder snapshotEncoder) chunk(chunk task.Chunk) error {
	if err := encoder.text("\n    {\n      \"cidr\": "); err != nil {
		return err
	}
	if err := encoder.value(chunk.CIDR); err != nil {
		return err
	}
	if err := encoder.text(",\n      \"cidr_name\": "); err != nil {
		return err
	}
	if err := encoder.value(chunk.CIDRName); err != nil {
		return err
	}
	if err := encoder.stringArray(",\n      \"ports\": ", chunk.Ports, "        "); err != nil {
		return err
	}
	if err := encoder.text(",\n      \"next_index\": " + strconv.Itoa(chunk.NextIndex) +
		",\n      \"scanned_count\": " + strconv.Itoa(chunk.ScannedCount) +
		",\n      \"total_count\": " + strconv.Itoa(chunk.TotalCount) +
		",\n      \"status\": "); err != nil {
		return err
	}
	if err := encoder.value(chunk.Status); err != nil {
		return err
	}
	return encoder.text("\n    }")
}

func (encoder snapshotEncoder) preScanPing(state *preScanPingEnvelope) error {
	if err := encoder.text(",\n  \"pre_scan_ping\": {\n    \"enabled\": " + strconv.FormatBool(*state.Enabled) +
		",\n    \"timeout_ms\": " + strconv.Itoa(*state.TimeoutMS)); err != nil {
		return err
	}
	if len(state.UnreachableIPv4U32) > 0 {
		if err := encoder.text(",\n    \"unreachable_ipv4_u32\": ["); err != nil {
			return err
		}
		for index, value := range state.UnreachableIPv4U32 {
			separator := "\n      "
			if index > 0 {
				separator = ",\n      "
			}
			if err := encoder.text(separator); err != nil {
				return err
			}
			if err := encoder.uint32(value); err != nil {
				return err
			}
		}
		if err := encoder.text("\n    ]"); err != nil {
			return err
		}
	}
	return encoder.text("\n  }")
}

func (encoder snapshotEncoder) output(output *OutputState) error {
	if err := encoder.text(",\n  \"output\": {\n    \"scan_path\": "); err != nil {
		return err
	}
	if err := encoder.value(output.ScanPath); err != nil {
		return err
	}
	if err := encoder.text(",\n    \"open_path\": "); err != nil {
		return err
	}
	if err := encoder.value(output.OpenPath); err != nil {
		return err
	}
	return encoder.text("\n  }")
}

func (encoder snapshotEncoder) targetExpansion(expansion *TargetExpansionState) error {
	return encoder.text(",\n  \"target_expansion\": {\n    \"candidate_count\": " + strconv.FormatUint(expansion.CandidateCount, 10) +
		",\n    \"candidate_limit\": " + strconv.FormatInt(expansion.CandidateLimit, 10) +
		",\n    \"memory_limit_gb\": " + strconv.FormatInt(expansion.MemoryLimitGB, 10) + "\n  }")
}

func (encoder snapshotEncoder) stringArray(prefix string, values []string, indentation string) error {
	if err := encoder.text(prefix); err != nil {
		return err
	}
	if values == nil {
		return encoder.text("null")
	}
	if err := encoder.text("["); err != nil {
		return err
	}
	for index, value := range values {
		separator := "\n" + indentation
		if index > 0 {
			separator = ",\n" + indentation
		}
		if err := encoder.text(separator); err != nil {
			return err
		}
		if err := encoder.value(value); err != nil {
			return err
		}
	}
	if len(values) > 0 {
		return encoder.text("\n" + indentation[:len(indentation)-2] + "]")
	}
	return encoder.text("]")
}

func (encoder snapshotEncoder) value(value string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = encoder.writer.Write(encoded)
	return err
}

func (encoder snapshotEncoder) text(value string) error {
	_, err := io.WriteString(encoder.writer, value)
	return err
}

func (encoder snapshotEncoder) uint32(value uint32) error {
	var buffer [10]byte
	encoded := strconv.AppendUint(buffer[:0], uint64(value), 10)
	for _, digit := range encoded {
		if err := encoder.writer.WriteByte(digit); err != nil {
			return err
		}
	}
	return nil
}
