// index命令组共用的AI客户端、超时、并发和进度辅助。
//
// 本文件只承载CLI层适配:
//   - 从已加载配置构造llm.Client;
//   - 把llm分类错误翻译为可操作提示;
//   - 计算单次调用超时与展示并发度;
//   - 把workflow进度安全输出到调用方指定Writer。
//
// 不读取仓库文件、不创建草稿、不修改索引或Baseline。
package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/llm"
	"github.com/aoci-spec/aoci-code/internal/workflow"
)

func buildAIClient(
	cfg *config.Config,
) (*llm.Client, error) {
	if !cfg.IsAIEnabled() {
		return nil, &ExitError{
			Code: ExitConfig,
			Err:  fmt.Errorf("%s", cliMessage("ai.client.disabled")),
		}
	}
	if cfg.AI.Model == "" {
		return nil, &ExitError{
			Code: ExitConfig,
			Err:  fmt.Errorf("%s", cliMessage("ai.client.model_missing")),
		}
	}

	options, keyMissing := configToLLMOptions(cfg)
	if keyMissing {
		return nil, &ExitError{
			Code: ExitConfig,
			Err: fmt.Errorf("%s", cliMessage(
				"ai.key_env_missing",
				cfg.AI.APIKeyEnv,
				cfg.AI.APIKeyEnv,
			)),
		}
	}

	client, err := llm.NewClient(options)
	if err != nil {
		return nil, &ExitError{
			Code: ExitConfig,
			Err:  err,
		}
	}

	return client, nil
}

func wrapAIErr(
	cfg *config.Config,
	err error,
) error {
	var llmError *llm.Error
	if err == nil ||
		!errors.As(err, &llmError) {
		return err
	}

	return fmt.Errorf(
		"%w\n%s",
		err,
		renderAIFailureHint(cfg, err),
	)
}

func singleCallTimeout(
	cfg *config.Config,
) time.Duration {
	if cfg.AI.TimeoutSeconds > 0 {
		return time.Duration(
			cfg.AI.TimeoutSeconds,
		) * time.Second
	}

	return llm.DefaultTimeout
}

func displayConcurrency(
	cfg *config.Config,
) int {
	if cfg.AI.MaxConcurrency > 0 {
		return cfg.AI.MaxConcurrency
	}

	return 1
}

func entriesProgressPrinter(
	writer io.Writer,
) workflow.ProgressFunc {
	return func(
		done,
		total int,
		path,
		status string,
	) {
		fmt.Fprintf(
			writer,
			"[%d/%d] %s %s\n",
			done,
			total,
			status,
			path,
		)
	}
}
