// hum1izer для pi: свод правил в системный промпт каждого хода, проверка
// файла - сразу после того, как его записал write или edit.
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent"
import { execFile } from "node:child_process"
import { promisify } from "node:util"

const run = promisify(execFile)
const BIN = process.env.HUM1IZER_BIN || "hum1izer"

async function ask(argv: string[], cwd?: string): Promise<string> {
  try {
    const { stdout } = await run(BIN, argv, { cwd })
    return stdout.trim()
  } catch {
    return ""
  }
}

export default function (pi: ExtensionAPI) {
  let rules = ""

  pi.on("before_agent_start", async (event) => {
    if (!rules) rules = await ask(["hook", "session-start", "--text"])
    if (!rules) return
    // Промпт приходит уже собранным, поэтому правила дописываются в конец
    // каждый ход: они не накапливаются, event.systemPrompt всегда исходный.
    return { systemPrompt: event.systemPrompt + "\n\n" + rules }
  })

  pi.on("tool_result", async (event, ctx) => {
    if (event.toolName !== "write" && event.toolName !== "edit") return
    const file = (event.input as { path?: string })?.path
    if (!file || event.isError) return
    const found = await ask(["hook", "post-edit", "--file", file], ctx.cwd)
    if (!found) return
    return { content: [...event.content, { type: "text" as const, text: found }] }
  })
}
