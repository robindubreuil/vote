#!/usr/bin/env node
// Bake the current git short hash into public/sw.js so the service worker
// busts its caches on every deploy. Reads scripts/sw.template.js (which
// contains the __BUILD_VERSION__ placeholder) and writes public/sw.js.

import { execSync } from 'child_process'
import { readFileSync, writeFileSync, mkdirSync } from 'fs'
import { dirname, join } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const rootDir = join(__dirname, '..')

let shortHash
try {
  shortHash = execSync('git rev-parse --short HEAD', { cwd: rootDir }).toString().trim()
} catch {
  shortHash = `dev-${Date.now()}`
}

const templatePath = join(__dirname, 'sw.template.js')
const outDir = join(rootDir, 'public')
const outPath = join(outDir, 'sw.js')

const template = readFileSync(templatePath, 'utf8')
const output = template.replace('__BUILD_VERSION__', shortHash)

mkdirSync(outDir, { recursive: true })
writeFileSync(outPath, output)
console.log(`Service worker written: ${outPath} (version=${shortHash})`)
