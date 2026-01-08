#!/usr/bin/env node
// Generate version info from git

import { execSync } from 'child_process'
import { writeFileSync } from 'fs'
import { dirname, join } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const rootDir = join(__dirname, '..')

let fullHash, shortHash, commitDate

try {
  fullHash = execSync('git rev-parse HEAD', { cwd: rootDir }).toString().trim()
  shortHash = execSync('git rev-parse --short HEAD', { cwd: rootDir }).toString().trim()
  commitDate = execSync('git log -1 --format=%cd --date=short', { cwd: rootDir }).toString().trim()
} catch {
  // Fallback if not in git repo
  fullHash = 'unknown'
  shortHash = 'unknown'
  commitDate = new Date().toISOString().split('T')[0]
}

const version = `// Version information - auto-generated
export const VERSION = {
  author: 'Robin DUBREUIL',
  license: 'MIT',
  commitHash: '${shortHash}',
  commitDate: '${commitDate}',
  fullHash: '${fullHash}'
}
`

writeFileSync(join(rootDir, 'shared/version.js'), version)
console.log(`Version info: ${shortHash} (${commitDate})`)
