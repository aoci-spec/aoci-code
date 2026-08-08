# Uninstall

Remove AOCI in two independent scopes.

## Remove the executable

Stop MCP host processes, remove the host's AOCI MCP registration, and then delete the installed binary or package. Confirm that shell and host configuration no longer resolve an unintended copy.

## Remove repository integration

Repository integration can include `aoci.txt`, `.aoci/`, an AOCI-managed block in `AGENTS.md`, and host configuration. These assets contain governance state and recovery evidence. Review and archive them before removal.

Removal is intentionally not automated in the current release candidate because
repository and host ownership differ and deletion is irreversible. Use normal
version-control review for tracked assets and the host's documented
configuration UI for MCP registration. Never recursively delete an unresolved
path or a repository root.
