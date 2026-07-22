import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'

export function normalizeConflictPaths(paths) {
  return [...new Set(paths.map((path) => path.trim()).filter(Boolean))].sort((a, b) =>
    a.localeCompare(b, 'en')
  )
}

export function conflictPathSignature(paths) {
  const normalized = normalizeConflictPaths(paths)
  return createHash('sha256').update(normalized.join('\n'), 'utf8').digest('hex')
}

export function classifyConflictIncident(currentSignature, nextSignature) {
  if (!currentSignature) {
    return 'new'
  }
  return currentSignature === nextSignature ? 'repeated' : 'changed'
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (process.argv[2] === '--classify') {
    process.stdout.write(`${classifyConflictIncident(process.argv[3] || '', process.argv[4] || '')}\n`)
  } else {
    const paths = readFileSync(0, 'utf8').split(/\r?\n/)
    process.stdout.write(`${conflictPathSignature(paths)}\n`)
  }
}
