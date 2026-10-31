package config

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// SetCognitionRefreshThreshold validates and sets the team-owned semantic-file
// threshold. The local configuration layer is never allowed to override it.
func (c *Config) SetCognitionRefreshThreshold(value int) error {
	if err := validateCognitionRefreshThreshold(value); err != nil {
		return err
	}
	c.CognitionRefreshThreshold = value
	return nil
}

func validateCognitionRefreshThreshold(value int) error {
	if value < machinecontract.CognitionRefreshThresholdMin ||
		value > machinecontract.CognitionRefreshThresholdMax {
		return fmt.Errorf(
			"cognition_refresh_threshold_out_of_range(value=%d,min=%d,max=%d)",
			value,
			machinecontract.CognitionRefreshThresholdMin,
			machinecontract.CognitionRefreshThresholdMax,
		)
	}
	return nil
}

// SetOverviewChunkTokens changes the sole project-owned Overview framing knob.
func (c *Config) SetOverviewChunkTokens(value int) error {
	candidate := OverviewDeliveryConfig{ChunkTokens: value}
	if err := validateOverviewDelivery(candidate); err != nil {
		return err
	}
	c.OverviewDelivery = candidate
	return nil
}

func validateOverviewDelivery(value OverviewDeliveryConfig) error {
	if value.ChunkTokens < machinecontract.OverviewChunkTokensMin ||
		value.ChunkTokens > machinecontract.OverviewChunkTokensMax {
		return fmt.Errorf(
			"overview_delivery_chunk_tokens_out_of_range(value=%d,min=%d,max=%d)",
			value.ChunkTokens,
			machinecontract.OverviewChunkTokensMin,
			machinecontract.OverviewChunkTokensMax,
		)
	}
	return nil
}

func (c *Config) DatabaseCognitionBatchLimits() (objects, evidenceBytes int) {
	objects = c.DatabaseCognitionBatchObjects
	if objects == 0 {
		objects = machinecontract.DatabaseCognitionBatchObjectsDefault
	}
	evidenceBytes = c.DatabaseCognitionBatchEvidenceBytes
	if evidenceBytes == 0 {
		evidenceBytes = machinecontract.DatabaseCognitionBatchEvidenceBytesDefault
	}
	return objects, evidenceBytes
}

// CodeCognitionBatchLimit resolves the team batch size for Code authoring:
// zero means the machine default.
func (c *Config) CodeCognitionBatchLimit() int {
	if c == nil || c.CodeCognitionBatchEntries == 0 {
		return machinecontract.CodeCognitionBatchEntriesDefault
	}
	return c.CodeCognitionBatchEntries
}

// SetCodeCognitionBatchEntries stores a validated team batch size; the wire
// ceiling stays EntriesBatchMaxItems.
func (c *Config) SetCodeCognitionBatchEntries(entries int) error {
	if entries < machinecontract.CodeCognitionBatchEntriesMin ||
		entries > machinecontract.CodeCognitionBatchEntriesMax {
		return fmt.Errorf("code_cognition_batch_entries_out_of_range(value=%d,min=%d,max=%d)",
			entries, machinecontract.CodeCognitionBatchEntriesMin, machinecontract.CodeCognitionBatchEntriesMax)
	}
	c.CodeCognitionBatchEntries = entries
	return nil
}

// SetHostTransportDefaults sizes the two transport settings for the host a
// new initialization names. Only Claude Code has a measured window wider than
// the defaults, and only fields still at their default are touched, so an
// explicit team value and every other host keep the conservative defaults.
func (c *Config) SetHostTransportDefaults(agent string) error {
	if agent != "claude" {
		return nil
	}
	if c.MaintainTransportBudgetBytes == 0 {
		if err := c.SetMaintainTransportBudgetBytes(machinecontract.MaintainTransportBudgetBytesClaudeCode); err != nil {
			return err
		}
	}
	if c.OverviewDelivery.ChunkTokens == 0 || c.OverviewDelivery.ChunkTokens == machinecontract.OverviewChunkTokensDefault {
		if err := c.SetOverviewChunkTokens(machinecontract.OverviewChunkTokensClaudeCode); err != nil {
			return err
		}
	}
	return nil
}

// MaintainTransportBudget resolves the team byte budget of one Maintain
// response: zero means the machine default.
func (c *Config) MaintainTransportBudget() int {
	if c == nil || c.MaintainTransportBudgetBytes == 0 {
		return machinecontract.MaintainTransportBudgetBytesDefault
	}
	return c.MaintainTransportBudgetBytes
}

// CodeCognitionParallelBatchLimit resolves how many current Code batches one
// logical plan may have outstanding at once: zero means one.
func (c *Config) CodeCognitionParallelBatchLimit() int {
	if c == nil || c.CodeCognitionParallelBatches == 0 {
		return machinecontract.CodeCognitionParallelBatchesDefault
	}
	return c.CodeCognitionParallelBatches
}

// SetCodeCognitionParallelBatches stores a validated team parallel-batch
// count; the wire ceiling is CodeCognitionParallelBatchesMax.
func (c *Config) SetCodeCognitionParallelBatches(batches int) error {
	if batches < machinecontract.CodeCognitionParallelBatchesMin ||
		batches > machinecontract.CodeCognitionParallelBatchesMax {
		return fmt.Errorf("code_cognition_parallel_batches_out_of_range(value=%d,min=%d,max=%d)",
			batches, machinecontract.CodeCognitionParallelBatchesMin, machinecontract.CodeCognitionParallelBatchesMax)
	}
	c.CodeCognitionParallelBatches = batches
	return nil
}

func validateCodeCognitionParallelBatches(cfg *Config) error {
	batches := cfg.CodeCognitionParallelBatchLimit()
	if batches < machinecontract.CodeCognitionParallelBatchesMin ||
		batches > machinecontract.CodeCognitionParallelBatchesMax {
		return fmt.Errorf("code_cognition_parallel_batches_out_of_range(value=%d,min=%d,max=%d)",
			batches, machinecontract.CodeCognitionParallelBatchesMin, machinecontract.CodeCognitionParallelBatchesMax)
	}
	return nil
}

// SetMaintainTransportBudgetBytes stores a validated team transport budget.
func (c *Config) SetMaintainTransportBudgetBytes(bytes int) error {
	if bytes < machinecontract.MaintainTransportBudgetBytesMin ||
		bytes > machinecontract.MaintainTransportBudgetBytesMax {
		return fmt.Errorf("maintain_transport_budget_bytes_out_of_range(value=%d,min=%d,max=%d)",
			bytes, machinecontract.MaintainTransportBudgetBytesMin, machinecontract.MaintainTransportBudgetBytesMax)
	}
	c.MaintainTransportBudgetBytes = bytes
	return nil
}

func validateMaintainTransportBudgetBytes(cfg *Config) error {
	bytes := cfg.MaintainTransportBudget()
	if bytes < machinecontract.MaintainTransportBudgetBytesMin ||
		bytes > machinecontract.MaintainTransportBudgetBytesMax {
		return fmt.Errorf("maintain_transport_budget_bytes_out_of_range(value=%d,min=%d,max=%d)",
			bytes, machinecontract.MaintainTransportBudgetBytesMin, machinecontract.MaintainTransportBudgetBytesMax)
	}
	return nil
}

func validateCodeCognitionBatchEntries(cfg *Config) error {
	entries := cfg.CodeCognitionBatchLimit()
	if entries < machinecontract.CodeCognitionBatchEntriesMin ||
		entries > machinecontract.CodeCognitionBatchEntriesMax {
		return fmt.Errorf("code_cognition_batch_entries_out_of_range(value=%d,min=%d,max=%d)",
			entries, machinecontract.CodeCognitionBatchEntriesMin, machinecontract.CodeCognitionBatchEntriesMax)
	}
	return nil
}

func validateDatabaseCognitionBatchLimits(cfg *Config) error {
	objects, evidenceBytes := cfg.DatabaseCognitionBatchLimits()
	if objects < machinecontract.DatabaseCognitionBatchObjectsMin ||
		objects > machinecontract.DatabaseCognitionBatchObjectsMax {
		return fmt.Errorf("database_cognition_batch_objects_out_of_range(value=%d,min=%d,max=%d)",
			objects, machinecontract.DatabaseCognitionBatchObjectsMin, machinecontract.DatabaseCognitionBatchObjectsMax)
	}
	if evidenceBytes < machinecontract.DatabaseCognitionBatchEvidenceBytesMin ||
		evidenceBytes > machinecontract.DatabaseCognitionBatchEvidenceBytesMax {
		return fmt.Errorf("database_cognition_batch_evidence_bytes_out_of_range(value=%d,min=%d,max=%d)",
			evidenceBytes, machinecontract.DatabaseCognitionBatchEvidenceBytesMin, machinecontract.DatabaseCognitionBatchEvidenceBytesMax)
	}
	return nil
}
