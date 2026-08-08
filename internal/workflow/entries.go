// Entries Draft批量编排。
//
// 单文件读取、策展验证、Prompt与草稿处理位于entries_worker.go。
// 特殊文件画像与include验证位于entries_curation.go。
//
// 本文件只负责:
//   - 前置条件;
//   - 策展资产现读;
//   - Worker池;
//   - 顺序稳定的结果汇总;
//   - Manifest和Ledger批次审计。
package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/llm"
)

const maxNeighborEntries = 3

// ProgressFunc 是批量Entry生成的进度回调。
type ProgressFunc func(
	done,
	total int,
	path,
	status string,
)

// EntriesOption 是RunEntriesDraft的可选扩展。
type EntriesOption func(*entriesOptions)

type entriesOptions struct {
	progress ProgressFunc
}

// WithProgress 注册进度回调。
func WithProgress(
	callback ProgressFunc,
) EntriesOption {
	return func(options *entriesOptions) {
		options.progress = callback
	}
}

// EntriesDraftResult 是一次批量Entry生成的结果。
type EntriesDraftResult struct {
	RunID        string
	Statuses     []draft.EntryStatus
	Drafted      int
	Warned       int
	Failed       int
	Skipped      int
	InputTokens  int
	OutputTokens int
	TokenSrc     string
}

type fileOutcome struct {
	status   draft.EntryStatus
	files    []string
	inTok    int
	outTok   int
	nonExact bool
}

// RunEntriesDraft 执行一批Entry候选生成。
func RunEntriesDraft(
	ctx context.Context,
	root string,
	cfg *config.Config,
	doc *index.Document,
	client *llm.Client,
	targets []string,
	oldEntries map[string]string,
	options ...EntriesOption,
) (*EntriesDraftResult, error) {
	if client == nil {
		return nil, errors.New(
			"llm客户端未注入(内部错误)",
		)
	}
	if len(targets) == 0 {
		return nil, errors.New(
			"目标清单为空: 用--missing或--paths提供目标",
		)
	}

	headerText, _ := index.ExtractHeader(
		doc.RawText,
	)
	if strings.TrimSpace(headerText) == "" {
		return nil, errors.New(
			"索引头部为空: 批量起草前必须先完成header bootstrap",
		)
	}

	curationDocument, err := loadEntryCurationDocument(
		root,
	)
	if err != nil {
		return nil, err
	}

	resolvedOptions := &entriesOptions{}
	for _, applyOption := range options {
		applyOption(resolvedOptions)
	}

	start := time.Now()

	runID, err := draft.NewRun(root)
	if err != nil {
		return nil, err
	}

	workers := cfg.AI.MaxConcurrency
	if workers <= 0 {
		workers = 1
	}
	if workers > len(targets) {
		workers = len(targets)
	}

	outcomes := make(
		[]fileOutcome,
		len(targets),
	)
	indexChannel := make(chan int)

	var waitGroup sync.WaitGroup
	var progressMutex sync.Mutex

	doneCount := 0

	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)

		go func(
			document *curation.Document,
		) {
			defer waitGroup.Done()

			for position := range indexChannel {
				outcomes[position] = draftOneEntry(
					ctx,
					root,
					runID,
					cfg,
					doc,
					client,
					headerText,
					targets[position],
					oldEntries,
					document,
				)

				if resolvedOptions.progress == nil {
					continue
				}

				progressMutex.Lock()
				doneCount++

				resolvedOptions.progress(
					doneCount,
					len(targets),
					outcomes[position].status.Path,
					outcomes[position].status.Status,
				)

				progressMutex.Unlock()
			}
		}(curationDocument)
	}

	for position := range targets {
		indexChannel <- position
	}
	close(indexChannel)
	waitGroup.Wait()

	result := &EntriesDraftResult{
		RunID:    runID,
		Statuses: []draft.EntryStatus{},
	}

	files := []string{}
	allExact := true

	for _, outcome := range outcomes {
		result.addStatus(outcome.status)
		files = append(
			files,
			outcome.files...,
		)
		result.InputTokens += outcome.inTok
		result.OutputTokens += outcome.outTok

		if outcome.nonExact {
			allExact = false
		}
	}

	result.TokenSrc = ledger.TokenSourceExact
	if !allExact {
		result.TokenSrc = ledger.TokenSourceEstimated
	}

	temperature := headerDraftTemperature

	manifest := &draft.Manifest{
		RunID:    runID,
		Kind:     draft.KindEntries,
		Model:    cfg.AI.Model,
		Provider: cfg.AI.Provider,
		EndpointHash: ledger.EndpointHash(
			cfg.AI.BaseURL,
		),
		Temperature:  &temperature,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		TokenSource:  result.TokenSrc,
		Entries:      result.Statuses,
		Files:        files,
	}

	if err := draft.SaveManifest(
		root,
		manifest,
	); err != nil {
		return nil, fmt.Errorf(
			"manifest落盘失败: %w",
			err,
		)
	}

	ledger.Append(
		root,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:            "entries_draft",
			Source:        ledger.SourceCLIAI,
			PathsCount:    len(targets),
			DurationMs:    time.Since(start).Milliseconds(),
			Model:         cfg.AI.Model,
			Provider:      cfg.AI.Provider,
			EndpointHash:  ledger.EndpointHash(cfg.AI.BaseURL),
			InputTokens:   result.InputTokens,
			OutputTokens:  result.OutputTokens,
			TokenSrc:      result.TokenSrc,
			DraftRunID:    runID,
			WarningsCount: result.Warned + result.Failed + result.Skipped,
		},
	)

	return result, nil
}

func (result *EntriesDraftResult) addStatus(
	status draft.EntryStatus,
) {
	result.Statuses = append(
		result.Statuses,
		status,
	)

	switch status.Status {
	case "drafted":
		result.Drafted++

	case "warned":
		result.Warned++

	case "failed":
		result.Failed++

	case "skipped":
		result.Skipped++
	}
}
