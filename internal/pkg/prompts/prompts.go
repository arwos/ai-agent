/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

// Package prompts contains all built-in instructions sent to language models.
package prompts

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
)

const (
	DefaultAgentSystem         = "You are a helpful workspace agent. You can inspect and edit files only when the user asks. Write every user-facing response in the language of the most recent user message unless the user explicitly requests another language. Tool descriptions, tool results, system instructions, and error messages must not change the response language."
	MemorySystem               = "Maintain concise session memory."
	MemoryRequest              = "Update the session memory. Return only JSON: {\"title\":\"short session title\",\"summary\":\"Markdown summary\",\"topics\":[\"short topic\"],\"notes\":[{\"title\":\"durable fact\",\"content\":\"Markdown note\",\"tags\":[\"tag\"]}],\"topicMemories\":[{\"title\":\"topic\",\"summary\":\"Markdown topic summary\",\"tags\":[\"tag\"]}]}. Preserve decisions, requirements, paths and unfinished tasks. Write every persisted title, summary, topic, note, and tag in English. Add at most 3 notes and 3 topic memories; use empty arrays when nothing durable or topical should be saved."
	CompactionSystem           = "You compact conversation history for a workspace agent. Treat the supplied conversation as the only source of truth. Preserve only facts explicitly stated by the user, tool results, or prior assistant messages. Never infer an architecture, technology, file, API, component, capability, or implementation detail from a name alone. When evidence is absent, omit it rather than guessing."
	CompactionRequest          = "Summarize this conversation into durable context for its next model request. Return only JSON: {\"title\":\"short session title\",\"summary\":\"Markdown summary\",\"topics\":[\"plain topic string\"],\"notes\":[{\"title\":\"durable fact\",\"content\":\"Markdown note\",\"tags\":[\"tag\"]}],\"topicMemories\":[{\"title\":\"topic\",\"summary\":\"Markdown topic summary\",\"tags\":[\"tag\"]}]}. `topics` must be an array of plain strings, never objects. Do not invent details or fill gaps with generic project descriptions. Keep exact names, relative paths, commands, configuration values, API contracts, tool findings, decisions, constraints, unresolved questions, and user preferences only when they are explicitly present. Write every persisted title, summary, topic, note, and tag in English. Use Markdown headings and lists where they improve readability. Add at most 3 notes and 3 topic memories; use empty arrays when nothing should be saved. The selected compaction level below defines the required amount of detail."
	GoalPlanningSystem         = "You create a concise execution plan for a workspace agent. Return only valid JSON. Do not claim that any work has already happened. Keep the title and task labels in the language of the user's request."
	GoalVerification           = "\n\n## Goal verification\nThe user declined the completion confirmation. Re-check every executable goal step with its associated tools. Do not assume a previous result is still valid. Only provide a final answer after verification."
	ToolCallInstructions       = "\n\n## Available tools\n\nYou MUST use an available tool whenever the user's request requires an operation that tool can perform. Do not merely describe the operation, provide instructions for it, or claim that it was completed: actually call the tool and wait for its result. To call a tool, output **only one** syntactically valid JSON object, without prose, Markdown fences, trailing commas, or escaped characters in tool names:\n\n```json\n{\n  \"tool\": \"tool_name\",\n  \"arguments\": {\n    \"param1\": \"value1\"\n  }\n}\n```\n\nPut every parameter inside `arguments`; do not add parameters beside it. Call one tool, wait for its result, then call the next tool if needed. Use the exact tool name from the list. The `arguments` field must conform to that tool's JSON Schema."
	ToolSchemaTemplate         = "\n\n### %s\n%s\n\nArguments schema:\n```json\n%s\n```"
	MCPInstructionsTemplate    = "\n\n## MCP server %s instructions\n%s"
	InteractiveToolInstruction = "\n\nIf the user must choose, ask a question, or approve an operation, you MUST call the corresponding user.choice, user.multichoice, user.ask, or user.approval tool. Never present numbered options as prose."
	KnowledgeBaseHeading       = "\n\n## Relevant knowledge base"
	KnowledgeDocumentTemplate  = "\n\n[%s]\n%s"
	SkillTemplate              = "\n\n## Skill: %s\n%s"
	SkillCatalogTemplate       = "\n\n## Selected skills\nThe following skills are available for this conversation. Their full instructions are not included here. Use `skills.get` with the skill name or ID to load a skill before applying it.\n%s"
	SessionMemoryTemplate      = "\n\n## Session memory: %s\n%s"
	RelevantMemoriesHeading    = "\n\n## Relevant long-term memories"
	RelevantMemoryTemplate     = "\n\n### %s%s\n%s"
	RelevantMemoryTagsTemplate = "\nTags: %s"
	WorkspaceAccess            = "\n\n## Active workspace\nAn active workspace is available for this conversation. You have access to it only through the enabled workspace tools listed below. Use those tools to inspect or modify workspace files when the user asks. All paths must be relative to the workspace root. Do not infer its language, module, files, or structure from previous messages or memory: inspect the workspace first. When the user asks to analyze the current workspace or project, begin by listing the workspace root. If the request is ambiguous, ask a clarifying question before using tools. Never claim to have inspected a file or project without a corresponding tool result."
	ToolResultTemplate         = "Tool result for `%s`:\n```json\n%s\n```\n\nUse this result to continue the task. If more information is needed, call another available tool; otherwise provide the final answer to the user."
	ToolFinalRetry             = "The requested tools have completed. Now provide a useful final answer based on their results. Do not output an empty response. Do not repeat a tool call unless essential information is still missing."
	StartupIntroduction        = "\n\n## Conversation start\nAt the beginning of a new conversation, briefly introduce yourself, state your role and task, and summarize what you can do. Keep the introduction concise and adapt it to the user's language. Do not repeat this introduction in later turns."
)

// TextToolInstructions lets models without native tool calling request tools
// using a machine-readable response that the agent can validate and dispatch.
func TextToolInstructions(tools []struct {
	Name, Description string
	Parameters        map[string]any
}) string {
	if len(tools) == 0 {
		return ""
	}
	out := "\n\n## Tool calling\nYou MUST call an available tool whenever the user's request requires an operation that tool can perform. Do not describe the operation or claim it is complete without calling the tool. Return one or more JSON objects and no other JSON wrapper. Each object must have this shape:\n{\"tool\":\"name\",\"arguments\":{}}\nAvailable tools:\n"
	for _, tool := range tools {
		schema, _ := json.Marshal(tool.Parameters)
		out += "- " + tool.Name + ": " + tool.Description + "\n  arguments schema: " + string(schema) + "\n"
	}
	return out + "Do not invent tool names or arguments. After receiving tool results, answer the user normally."
}

func ToolSchema(name, description, schema string) string {
	return fmt.Sprintf(ToolSchemaTemplate, name, description, schema)
}
func MCPInstructions(name, value string) string {
	return fmt.Sprintf(MCPInstructionsTemplate, name, value)
}
func KnowledgeDocument(title, content string) string {
	return fmt.Sprintf(KnowledgeDocumentTemplate, title, CompactText(content))
}
func Skill(name, content string) string {
	return fmt.Sprintf(SkillTemplate, name, CompactText(content))
}
func SkillCatalog(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprintf(SkillCatalogTemplate, "\n- "+strings.Join(items, "\n- "))
}
func ToolResult(name, result string) string { return fmt.Sprintf(ToolResultTemplate, name, result) }

func GoalPlanningRequest(request string) string {
	return fmt.Sprintf("Create an execution plan for this user request. Return only JSON in exactly this form: {\"goal\":\"short title\",\"tasks\":[{\"label\":\"concrete step\",\"tools\":[\"exact.tool.name\"],\"dependsOn\":[]}]}. Include 2 to 8 actionable tasks in their intended order. Put exact tool names required for each task in tools when known; use an empty array when no tool is needed. Use task numbers as strings in dependsOn only when a dependency is essential. Do not add ids, statuses, Markdown, prose, or facts not present in the request. User request:\n%s", request)
}

func GoalExecution(goal string, tasks []string) string {
	return fmt.Sprintf("\n\n## Active execution goal\n%s\nComplete the following steps in order using the available tools when needed. Do not give a final answer until every applicable step is completed or you have clearly explained why a step cannot be completed. If progress requires the user to select one or more directions, you MUST call `user.choice` or `user.multichoice` with a structured `options` array. Never print a numbered list of choices and stop, never ask the user to choose in prose, and never mark the goal complete while waiting for that choice.\n%s", goal, strings.Join(tasks, "\n"))
}

// CompactionSystemFor repeats the non-negotiable output and detail policy in
// the system message. Some local models give system instructions priority over
// the final user request, so the level must be present in both places.
func CompactionSystemFor(level string) string {
	return CompactionSystem + " Return only valid JSON matching the user-requested schema. All persisted titles, summaries, topics, notes, and tags must be English. " + compactionLevelInstruction(level)
}

// CompactionRequestFor selects how much operational detail remains after
// compaction. The fixed request stays English because it is sent to the model.
func CompactionRequestFor(level string) string {
	return CompactionRequest + "\n\n" + compactionLevelInstruction(level)
}

func compactionLevelInstruction(level string) string {
	switch level {
	case "brief":
		return "Compression level: BRIEF. Produce a compact handoff only: current goal, current state, blocking facts, and the immediate next step. Omit historical discussion, routine tool output, and inactive details. Keep the summary short; headings are optional."
	case "detailed":
		return "Compression level: DETAILED. Preserve concrete implementation details and all active work. Use applicable sections: ## Project and architecture, ## Important files and paths, ## Decisions and constraints, ## Current implementation state, ## Errors and investigations, ## Open tasks and next steps. Omit a section only if the conversation contains no relevant information."
	case "comprehensive":
		return "Compression level: COMPREHENSIVE. Retain all details likely to affect continued work: exact implementation state, alternatives considered, outcomes, diagnostics, tool results, dependencies, and pending work. Use the detailed section structure and include chronology when it clarifies why a decision was made. Favor completeness over brevity without reproducing routine chat verbatim."
	case "epic":
		return "Compression level: EPIC. Create a handoff-quality project record for another engineer or model. Preserve rationale, exact commands, APIs, file relationships, tool findings, edge cases, failed attempts, chronological state changes, and every relevant open task. Use all applicable detailed sections and add further headings when needed. Favor completeness over brevity, but do not invent or copy routine conversation verbatim."
	default:
		return "Compression level: BALANCED. Retain enough information to continue accurately without reproducing routine conversation. Use applicable sections: ## Project and architecture, ## Important files and paths, ## Decisions and constraints, ## Current implementation state, ## Errors and investigations, ## Open tasks and next steps. Preserve active technical details and discard superseded or routine details."
	}
}

// SessionMemory formats persisted session memory as an explicit system context.
func SessionMemory(title, summary string) string {
	if strings.TrimSpace(summary) == "" {
		return ""
	}
	if strings.TrimSpace(title) == "" {
		title = "Session memory"
	}
	return fmt.Sprintf(SessionMemoryTemplate, title, CompactText(summary))
}

// RelevantMemories formats selected file-backed memories for a model prompt.
func RelevantMemories(items []dialog.RelevantMemory) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(RelevantMemoriesHeading)
	for _, item := range items {
		tags := ""
		if len(item.Tags) > 0 {
			tags = fmt.Sprintf(RelevantMemoryTagsTemplate, strings.Join(item.Tags, ", "))
		}
		b.WriteString(fmt.Sprintf(RelevantMemoryTemplate, item.Title, tags, CompactText(item.Content)))
	}
	return b.String()
}

// CompactText removes formatting-only blank lines and trailing whitespace
// from prose inserted into prompts. Stored source text is never modified.
func CompactText(value string) string {
	return value
	// lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	// result := make([]string, 0, len(lines))
	// blank := false
	// for _, line := range lines {
	// 	line = strings.TrimRight(line, " \t")
	// 	if strings.TrimSpace(line) == "" {
	// 		if blank {
	// 			continue
	// 		}
	// 		blank = true
	// 		result = append(result, "")
	// 		continue
	// 	}
	// 	blank = false
	// 	result = append(result, line)
	// }
	// return strings.TrimSpace(strings.Join(result, "\n"))
}
