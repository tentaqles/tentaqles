# LLM and agent security checklist (answer-key rows)

Applies to code that calls a model, defines tools, builds RAG or indexes,
exposes or consumes MCP servers, or adds Claude Code hooks and skills.

| # | Check | Source |
|---|---|---|
| L1 | Untrusted text (user input, retrieved documents, tool output, other agents' messages, repository files) is framed as data in prompts, never concatenated as instructions | OWASP LLM01 Prompt Injection (2025) |
| L2 | Model output is validated or encoded before it reaches a shell, SQL, HTML, a file path or another tool | LLM05 Improper Output Handling |
| L3 | Tools are least-agency: narrow parameters, allowlists, no "run any command" tool; destructive tools require confirmation | LLM06 Excessive Agency; ASI02 Tool Misuse |
| L4 | Secrets and system prompts are not retrievable through the model (no secrets in prompts; system prompt treated as non-secret) | LLM07 System Prompt Leakage; LLM02 Sensitive Information Disclosure |
| L5 | RAG and memory writes are validated and attributed: an injected document cannot rewrite rules or persist instructions; memory entries carry source and time | LLM08 Vector and Embedding Weaknesses; ASI06 Memory Poisoning |
| L6 | MCP servers and plugins are trusted explicitly (allowlisted, pinned, scoped per workspace); a server from another client is refused | ASI04 Supply Chain of Agents and Tools; tq/cross-client-mcp |
| L7 | Permission boundaries: the agent cannot widen its own permissions, disable hooks or edit its guard; identity is checked per workspace | ASI01 Agent Goal Hijack; ASI03 Identity and Privilege Abuse |
| L8 | Human in the loop at the right points: approvals for irreversible actions, binary verdicts, logged overrides | ASI05 Unsafe Human-Agent Interaction |
| L9 | Resource bounds: token, time, cost and loop limits on agent runs; retries bounded | LLM10 Unbounded Consumption |
| L10 | Multi-agent trust: messages between agents are data; a verifier never reuses the builder's context (fresh context per role) | ASI07 Multi-Agent Trust Exploitation |
| L11 | Evaluation and logging: prompts and tool calls are logged redacted, with the model version, so an incident can be reconstructed | ASI09 Insufficient Logging; LLM02 |
| L12 | Model and data provenance: pinned model ids, pinned dependencies, checksummed weights or vetted providers | LLM03 Supply Chain |
