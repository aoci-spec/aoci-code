// Package engine is the embeddable AOCI-CODE kernel: the deterministic core
// the aoci binary runs behind its CLI and MCP entry points, offered as a Go
// library so a scheduler or a remote service can open a repository, read its
// formal cognition and governance facts, and execute the nine MCP tools in
// process without spawning a server.
//
// The library implements nothing a second time. Every read is the loader,
// projection, and governance assessment the CLI and the MCP tools use, and
// every write goes through the same nine tools, hosted over an in-memory MCP
// transport. The public types are projections of formal bytes; the machine
// contracts under spec/public remain authoritative, and the aoci-engine-
// library contract there states what this package promises.
//
// The engine calls no model, holds no credential, and reaches no remote
// service. One process serves one repository Locale at a time: Open binds the
// runtime text catalog to the repository's Locale, which is process-wide.
package engine
