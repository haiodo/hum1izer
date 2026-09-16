// hum1izer для opencode: свод правил уходит в системный промпт, а после
// каждой правки файла проверяется только этот файл.
import { execFile } from "node:child_process"
import { promisify } from "node:util"

const run = promisify(execFile)
const BIN = process.env.HUM1IZER_BIN || "hum1izer"

// Имя аргумента с путём у edit и write разное в разных версиях, берём первое
// непустое, а не угадываем одно.
const pathOf = (args) => args?.filePath || args?.path || args?.file_path || ""

async function ask(argv, cwd) {
  try {
    const { stdout } = await run(BIN, argv, { cwd })
    return stdout.trim()
  } catch {
    return ""
  }
}

export const Hum1izer = async ({ directory }) => ({
  "experimental.chat.system.transform": async (_input, output) => {
    const rules = await ask(["hook", "session-start", "--text"], directory)
    if (rules) output.system.push(rules)
  },
  "tool.execute.after": async (input, output) => {
    if (input.tool !== "edit" && input.tool !== "write") return
    const file = pathOf(input.args)
    if (!file) return
    const found = await ask(["hook", "post-edit", "--file", file], directory)
    if (found) output.output += "\n\n" + found
  },
})
