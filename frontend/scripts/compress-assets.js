#!/usr/bin/env node

/**
 * Post-build compression script
 * Creates pre-compressed versions of static assets (.gz, .br)
 * Uses native compression tools for maximum efficiency
 */

import { execSync } from 'child_process';
import { readdir, stat } from 'fs/promises';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
// Get dist directory from first argument or default to current directory
const DIST_DIR = process.argv[2] ? join(process.cwd(), process.argv[2]) : join(__dirname, '../dist');

// Compression configuration
const COMPRESSORS = {
  gzip: {
    ext: '.gz',
    cmd: 'gzip',
    args: ['-9', '-k', '-f'],
    check: () => checkCommand('gzip')
  },
  brotli: {
    ext: '.br',
    cmd: 'brotli',
    args: ['-q', '11', '-k', '-f'],
    check: () => checkCommand('brotli')
  }
};

// File extensions to compress
const COMPRESS_EXTENSIONS = new Set([
  '.html',
  '.css',
  '.js',
  '.json',
  '.svg',
  '.xml',
  '.webmanifest',
  '.mjs'
]);

// File size threshold (bytes) - skip very small files
const MIN_SIZE = 200;

async function checkCommand(cmd) {
  try {
    execSync(`command -v ${cmd}`, { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
}

async function getFilesToCompress(dir) {
  const files = [];
  const entries = await readdir(dir, { withFileTypes: true });

  for (const entry of entries) {
    const fullPath = join(dir, entry.name);

    if (entry.isDirectory()) {
      if (!entry.name.startsWith('.') && entry.name !== 'node_modules') {
        files.push(...await getFilesToCompress(fullPath));
      }
    } else if (entry.isFile()) {
      const ext = entry.name.toLowerCase();
      const hasValidExt = [...COMPRESS_EXTENSIONS].some(e => ext.endsWith(e));
      const isAlreadyCompressed = ext.endsWith('.gz') || ext.endsWith('.br');

      if (hasValidExt && !isAlreadyCompressed) {
        const stats = await stat(fullPath);
        if (stats.size >= MIN_SIZE) {
          files.push(fullPath);
        }
      }
    }
  }

  return files;
}

function compressFile(filePath, compressor) {
  try {
    const start = Date.now();
    execSync(`${compressor.cmd} ${compressor.args.join(' ')} "${filePath}"`, {
      stdio: 'ignore',
      cwd: dirname(filePath)
    });
    return { success: true, duration: Date.now() - start };
  } catch (error) {
    return { success: false, error: error.message };
  }
}

async function compressAll() {
  console.log('Starting asset compression...\n');

  const available = {};
  for (const [name, config] of Object.entries(COMPRESSORS)) {
    if (await config.check()) {
      available[name] = config;
      console.log(`  ${name} available`);
    } else {
      console.log(`  ${name} not found (install: apt install ${name})`);
    }
  }

  if (Object.keys(available).length === 0) {
    console.error('\nNo compression tools available!');
    process.exit(1);
  }

  console.log(`\nScanning ${DIST_DIR}...`);
  const files = await getFilesToCompress(DIST_DIR);
  console.log(`  Found ${files.length} files to compress\n`);

  let totalCompressed = 0;
  const startTime = Date.now();

  for (const filePath of files) {
    const relativePath = filePath.replace(DIST_DIR + '/', '');

    for (const [name, compressor] of Object.entries(available)) {
      const result = compressFile(filePath, compressor);
      if (result.success) {
        totalCompressed++;
      }
    }
    process.stdout.write(`\r  Progress: ${totalCompressed}/${files.length * Object.keys(available).length} files`);
  }

  const duration = ((Date.now() - startTime) / 1000).toFixed(2);

  console.log(`\n\nCompression complete!`);
  console.log(`  Total: ${totalCompressed} files`);
  console.log(`  Time: ${duration}s`);
  console.log(`  Formats: ${Object.keys(available).join(', ')}`);
}

compressAll().catch(console.error);
