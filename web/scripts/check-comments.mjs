import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const roots = ['src', 'e2e', 'scripts']
const configs = ['vite.config.ts', 'playwright.config.ts']
const sourcePattern = /\.(ts|tsx|mjs)$/

function collect(dir) {
  const files = []
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) files.push(...collect(full))
    else if (sourcePattern.test(entry.name)) files.push(full)
  }
  return files
}

function commentLines(file) {
  const text = fs.readFileSync(file, 'utf8')
  const kind = file.endsWith('.tsx') ? ts.ScriptKind.TSX : file.endsWith('.mjs') ? ts.ScriptKind.JS : ts.ScriptKind.TS
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind)
  const starts = new Set()
  const record = (ranges) => {
    for (const range of ranges ?? []) {
      if (!text.startsWith('///', range.pos)) starts.add(range.pos)
    }
  }
  const visit = (node) => {
    if (node.kind !== ts.SyntaxKind.JsxText) {
      record(ts.getLeadingCommentRanges(text, node.pos))
      record(ts.getTrailingCommentRanges(text, node.end))
    }
    for (const child of node.getChildren(source)) visit(child)
  }
  visit(source)
  return [...starts].map((pos) => source.getLineAndCharacterOfPosition(pos).line + 1).sort((a, b) => a - b)
}

const files = [...roots.flatMap((r) => collect(path.join(webRoot, r))), ...configs.map((c) => path.join(webRoot, c))]
const found = files.flatMap((file) => commentLines(file).map((line) => `${path.relative(webRoot, file)}:${line}`))

if (found.length > 0) {
  console.error(`code comments are not allowed; remove them:\n${found.join('\n')}`)
  process.exit(1)
}
console.log(`no comments in ${files.length} files`)
