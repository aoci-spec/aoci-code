package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestStageRequestBodyLimitsUseMachineContract(t *testing.T) {
	tests := []struct {
		name string
		max  int
		read func(io.Reader) error
	}{
		{
			name: "header",
			max:  machinecontract.HeaderRequestMaxBytes,
			read: func(reader io.Reader) error {
				_, err := readAgentHeaderStageRequest(reader)
				return err
			},
		},
		{
			name: "entries",
			max:  machinecontract.EntriesRequestMaxBytes,
			read: func(reader io.Reader) error {
				_, err := readAgentStageRequest(reader)
				return err
			},
		},
		{
			name: "curation",
			max:  machinecontract.CurationRequestMaxBytes,
			read: func(reader io.Reader) error {
				_, err := readAgentCurationStageRequest(reader)
				return err
			},
		},
	}

	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			err := current.read(bytes.NewReader(make([]byte, current.max+1)))
			if err == nil ||
				!strings.Contains(err.Error(), "超过上限") ||
				!strings.Contains(err.Error(), fmt.Sprint(current.max)) {
				t.Fatalf("请求体机器上限未生效: max=%d err=%v", current.max, err)
			}
		})
	}
}

func TestStageFieldAndBatchLimitsUseMachineContract(t *testing.T) {
	planID := strings.Repeat("a", 64)

	header := agentHeaderStageRequest{
		Version: agentHeaderStageVersion,
		PlanID:  planID,
		Agent:   "codex",
		Header:  strings.Repeat("#", machinecontract.HeaderTextMaxBytes),
	}
	if err := normalizeAndValidateAgentHeaderStageRequest(&header); err != nil {
		t.Fatalf("header字段恰好机器上限应放行: %v", err)
	}
	header.Header += "#"
	if err := normalizeAndValidateAgentHeaderStageRequest(&header); err == nil ||
		!strings.Contains(err.Error(), fmt.Sprint(machinecontract.HeaderTextMaxBytes)) {
		t.Fatalf("header字段超过机器上限应拒绝: %v", err)
	}

	entries := agentStageRequest{
		Version: agentStageVersion,
		PlanID:  planID,
		Agent:   "codex",
		Entries: make([]agentStageEntry, machinecontract.EntriesBatchMaxItems),
	}
	if err := normalizeAndValidateAgentStageRequest(&entries); err != nil {
		t.Fatalf("Entries批次恰好机器上限应放行: %v", err)
	}
	entries.Entries = append(entries.Entries, agentStageEntry{})
	if err := normalizeAndValidateAgentStageRequest(&entries); err == nil ||
		!strings.Contains(err.Error(), fmt.Sprint(machinecontract.EntriesBatchMaxItems)) {
		t.Fatalf("Entries批次超过机器上限应拒绝: %v", err)
	}

	curation := agentCurationStageRequest{
		Version:   agentCurationStageVersion,
		PlanID:    planID,
		Agent:     "codex",
		Decisions: make([]agentCurationDecision, machinecontract.CurationBatchMaxItems),
	}
	if err := normalizeAgentCurationRequest(&curation); err != nil {
		t.Fatalf("Curation批次恰好机器上限应放行: %v", err)
	}
	curation.Decisions = append(curation.Decisions, agentCurationDecision{})
	if err := normalizeAgentCurationRequest(&curation); err == nil ||
		!strings.Contains(err.Error(), fmt.Sprint(machinecontract.CurationBatchMaxItems)) {
		t.Fatalf("Curation批次超过机器上限应拒绝: %v", err)
	}
}
