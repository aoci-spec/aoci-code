// CLI稳定Long Help文本资产消费者。
//
// Cobra命令仍由各业务文件创建；各命令在构造时直接从本文件取得稳定Long
// Help。此处同时承载本次迁入资源的观察命令Short；其他Short、Flag Usage、
// 动态报告、错误分支和权限控制仍留在原Go文件。
//
// 这种接法保证textassets是Long Help唯一事实源，不在业务Go文件中保留一份
// 不再生效但仍可能漂移的重复自然语言。
package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

const (
	cliHelpAssetFailurePrefix   = "[text asset"
	cliHelpAssetErrorAnnotation = "aoci.cli/help-asset-error"
)

// rootLongHelp返回根命令稳定说明。
func rootLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpRootLong,
	)
}

// doctorLongHelp返回Doctor稳定说明。
func doctorLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpDoctorLong,
	)
}

// indexLongHelp返回Index工序组稳定说明。
func indexLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpIndexLong,
	)
}

// indexUpdateLongHelp返回Index Update稳定说明。
func indexUpdateLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpIndexUpdateLong,
	)
}

// verifyLongHelp返回Verify四态与治理债务稳定说明。
func verifyLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpVerifyLong,
	)
}

// verifyShortHelp returns the stable Verify command summary.
func verifyShortHelp() string {
	return cliHelpText(
		textassets.ContractHelpVerifyShort,
	)
}

// checkLongHelp返回Check提交阻断稳定说明。
func checkLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpCheckLong,
	)
}

// checkShortHelp returns the stable Check command summary.
func checkShortHelp() string {
	return cliHelpText(
		textassets.ContractHelpCheckShort,
	)
}

// scanLongHelp返回Scan防洗白稳定说明。
func scanLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpScanLong,
	)
}

// removeEntryLongHelp返回人工删除Entry稳定说明。
func removeEntryLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpRemoveEntryLong,
	)
}

// aiLongHelp返回AI增强层稳定总说明。
func aiLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpAILong,
	)
}

// aiSetupLongHelp返回AI配置写入与密钥纪律稳定说明。
func aiSetupLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpAISetupLong,
	)
}

// aiTestLongHelp返回AI端点最小探测稳定说明。
func aiTestLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpAITestLong,
	)
}

// indexBuildLongHelp返回Entries起草目标选择稳定说明。
func indexBuildLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpIndexBuildLong,
	)
}

// headerDraftLongHelp返回Header Draft-first稳定说明。
func headerDraftLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpHeaderDraftLong,
	)
}

// indexScoreLongHelp返回九维度评分稳定说明。
func indexScoreLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpIndexScoreLong,
	)
}

// indexScoreShortHelp returns the stable Index Score command summary.
func indexScoreShortHelp() string {
	return cliHelpText(
		textassets.ContractHelpIndexScoreShort,
	)
}

// indexInventoryLongHelp returns the Inventory observation description.
func indexInventoryLongHelp() string {
	return observationLongHelp(
		textassets.ContractHelpIndexInventoryLong,
	)
}

// indexInventoryShortHelp returns the stable Inventory command summary.
func indexInventoryShortHelp() string {
	return cliHelpText(
		textassets.ContractHelpIndexInventoryShort,
	)
}

// readObservationAuditHelp is the single CLI and documentation source for the
// distinction between formal-asset immutability and local audit writes.
func readObservationAuditHelp() string {
	return cliHelpText(
		textassets.ContractHelpReadObservationAudit,
	)
}

// observationLongHelp appends the shared audit boundary to one command's
// command-specific behavior description.
func observationLongHelp(id textassets.ID) string {
	return cliHelpText(id) + "\n" +
		readObservationAuditHelp()
}

// indexAgentLongHelp返回Host-Agent协议能力稳定说明。
func indexAgentLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpIndexAgentLong,
	)
}

// indexAgentHeaderLongHelp返回Host-Agent Header接入稳定说明。
func indexAgentHeaderLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpIndexAgentHeaderLong,
	)
}

// updateEntryLongHelp返回单条Entry写入稳定说明。
func updateEntryLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpUpdateEntryLong,
	)
}

// entriesCheckLongHelp返回Entries Check治理稳定说明。
func entriesCheckLongHelp() string {
	return cliHelpText(
		textassets.ContractHelpEntriesCheckLong,
	)
}

// cliHelpText渲染无动态变量的稳定Help标量。
func cliHelpText(
	id textassets.ID,
) string {
	return cliHelpTemplate(id, nil)
}

func cliMessage(key string, args ...any) string {
	value, err := textassets.Message(textassets.ActiveLocale(), key, args...)
	if err != nil {
		return fmt.Sprintf("[text_asset_error:%s]", key)
	}
	return value
}

// cliHelpTemplate用于Cobra只能接收string的Help元数据。资源损坏时返回包含
// ID与底层错误的可见诊断，不panic、不回退到硬编码正文，也不阻断无关命令。
func cliHelpTemplate(id textassets.ID, data any) string {
	value, err := textassets.RenderScalar(
		textassets.ActiveLocale(),
		id,
		data,
	)
	if err != nil {
		return fmt.Sprintf("[text_asset_error:%s]", id)
	}

	return value
}

// installCLIHelpAssetGuard把Cobra只能接收string的资源错误恢复为真实命令错误。
//
// 闸门只检查本次Help实际渲染的命令字段和参数说明，不扫描兄弟命令，避免一个
// 无关Help资产损坏阻断其他命令。检查发生在Cobra写出任何Help正文之前。
func installCLIHelpAssetGuard(root *cobra.Command) {
	if root == nil {
		return
	}

	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(command *cobra.Command, args []string) {
		if err := commandHelpAssetError(command); err != nil {
			if root.Annotations == nil {
				root.Annotations = map[string]string{}
			}
			root.Annotations[cliHelpAssetErrorAnnotation] =
				err.Error()

			return
		}

		if root.Annotations != nil {
			delete(root.Annotations, cliHelpAssetErrorAnnotation)
		}
		defaultHelp(command, args)
	})
}

// cliHelpExecutionError取得本次Help渲染拦截到的错误。状态绑定单棵命令树，
// 每次读取即清除，避免跨调用永久缓存。
func cliHelpExecutionError(root *cobra.Command) error {
	if root == nil || root.Annotations == nil {
		return nil
	}

	message := root.Annotations[cliHelpAssetErrorAnnotation]
	delete(root.Annotations, cliHelpAssetErrorAnnotation)
	if message == "" {
		return nil
	}

	return &ExitError{
		Code: ExitInternal,
		Err:  errors.New(message),
	}
}

// commandHelpAssetError返回当前命令Help会消费的首个资源错误。
func commandHelpAssetError(command *cobra.Command) error {
	if command == nil {
		return nil
	}

	for _, text := range []string{
		command.Long,
		command.Short,
		command.Example,
	} {
		if isCLIHelpAssetFailure(text) {
			return errors.New(text)
		}
	}

	for _, usages := range []string{
		command.NonInheritedFlags().FlagUsages(),
		command.InheritedFlags().FlagUsages(),
	} {
		position := strings.Index(usages, cliHelpAssetFailurePrefix)
		if position < 0 {
			continue
		}
		message := usages[position:]
		if end := strings.IndexByte(message, '\n'); end >= 0 {
			message = message[:end]
		}
		return errors.New(strings.TrimSpace(message))
	}

	return nil
}

func isCLIHelpAssetFailure(text string) bool {
	return strings.HasPrefix(
		strings.TrimSpace(text),
		cliHelpAssetFailurePrefix,
	)
}

// findCLICommand从给定根命令逐层按Name定位子命令。
//
// 该函数用于字节级Help测试和其他只读命令树核对，不修改命令。
func findCLICommand(
	root *cobra.Command,
	path ...string,
) *cobra.Command {
	current := root

	for _, name := range path {
		if current == nil {
			return nil
		}

		var found *cobra.Command

		for _, candidate := range current.Commands() {
			if candidate.Name() == name {
				found = candidate
				break
			}
		}

		if found == nil {
			return nil
		}

		current = found
	}

	return current
}
