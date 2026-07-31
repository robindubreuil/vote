#!/usr/bin/env node
// Generate version info for the frontend footer.
//
// Precedence: explicit env vars (set by CI build-args / debian/rules) →
// git (local dev, and CI checkouts that still carry .git) → "unknown".
// The env-var path is what makes the footer correct in Docker, where
// .git/ is excluded from the build context (.dockerignore) so `git
// rev-parse` would otherwise fail and fall back to "unknown". The .deb
// build hits the same failure: dpkg-buildpackage runs as root while the
// mounted .git is owned by the runner user, tripping git's
// safe.directory (dubious-ownership) check.

import { execSync } from 'child_process'
import { writeFileSync } from 'fs'
import { dirname, join } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const rootDir = join(__dirname, '../..')

function git(cmd) {
  try {
    return execSync(cmd, { cwd: rootDir, stdio: ['ignore', 'pipe', 'ignore'] }).toString().trim()
  } catch {
    return ''
  }
}

function fmtDate(iso) {
  if (!iso) return ''
  const m = /^(\d{4}-\d{2}-\d{2})/.exec(iso)
  return m ? m[1] : iso
}

const shortHash =
  process.env.VOTE_GIT_COMMIT ||
  git('git rev-parse --short HEAD') ||
  'unknown'
const fullHash =
  process.env.VOTE_GIT_FULL_COMMIT ||
  git('git rev-parse HEAD') ||
  (shortHash === 'unknown' ? 'unknown' : shortHash)
const commitDate =
  fmtDate(process.env.VOTE_BUILD_DATE) ||
  git('git log -1 --format=%cd --date=short') ||
  new Date().toISOString().split('T')[0]

const version = `// Version information - auto-generated
export const VERSION = {
  author: 'Robin DUBREUIL',
  license: 'MIT',
  commitHash: '${shortHash}',
  commitDate: '${commitDate}',
  fullHash: '${fullHash}'
}
`

writeFileSync(join(rootDir, 'frontend/shared/version.js'), version)
console.log(`Version info: ${shortHash} (${commitDate})`)
