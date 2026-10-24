'use strict';

const fs = require('fs');
const path = require('path');
const os = require('os');
const https = require('https');
const http = require('http');
const crypto = require('crypto');
const { execFile } = require('child_process');
const { getPlatformInfo } = require('./platform');

const GITHUB_REPO = 'justwasm/pigasus';

/**
 * Resolves the cache directory where Pigasus binaries are stored.
 */
function getCacheDir() {
  if (process.env.PIGASUS_CACHE_DIR) {
    return process.env.PIGASUS_CACHE_DIR;
  }
  const home = os.homedir();
  return path.join(home, '.cache', 'pigasus', 'bin');
}

/**
 * Recursively locates the binary inside an extracted directory.
 * Handles nested archive structures like dist/pigasus_v1.4.6_arm64/pigasus.
 */
function findBinary(dir, targetName) {
  const entries = fs.readdirSync(dir, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      const found = findBinary(fullPath, targetName);
      if (found) return found;
    } else if (entry.isFile() && (entry.name === targetName || entry.name === `${targetName}.exe`)) {
      return fullPath;
    }
  }
  return null;
}

/**
 * Follows HTTP/HTTPS redirects and downloads to a local writable stream.
 */
function downloadFile(url, destStream, onProgress) {
  return new Promise((resolve, reject) => {
    function get(currentUrl, redirectCount) {
      if (redirectCount > 10) {
        reject(new Error(`Too many redirects when fetching ${url}`));
        return;
      }

      const client = currentUrl.startsWith('https:') ? https : http;
      client.get(currentUrl, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          const nextUrl = new URL(res.headers.location, currentUrl).toString();
          res.resume(); // Discard data
          get(nextUrl, redirectCount + 1);
          return;
        }

        if (res.statusCode !== 200) {
          res.resume();
          reject(new Error(`Download failed with HTTP ${res.statusCode} from ${currentUrl}`));
          return;
        }

        const totalBytes = parseInt(res.headers['content-length'] || '0', 10);
        let downloadedBytes = 0;

        res.on('data', (chunk) => {
          downloadedBytes += chunk.length;
          if (onProgress && totalBytes > 0) {
            onProgress(downloadedBytes, totalBytes);
          }
        });

        res.pipe(destStream);

        destStream.on('finish', () => resolve());
        destStream.on('error', (err) => reject(err));
        res.on('error', (err) => reject(err));
      }).on('error', (err) => reject(err));
    }

    get(url, 0);
  });
}

/**
 * Fetches string content (e.g. checksums.txt) with redirect support.
 */
function fetchText(url) {
  return new Promise((resolve, reject) => {
    function get(currentUrl, redirectCount) {
      if (redirectCount > 10) {
        return reject(new Error('Too many redirects'));
      }
      const client = currentUrl.startsWith('https:') ? https : http;
      client.get(currentUrl, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          const nextUrl = new URL(res.headers.location, currentUrl).toString();
          res.resume();
          return get(nextUrl, redirectCount + 1);
        }
        if (res.statusCode !== 200) {
          res.resume();
          return reject(new Error(`HTTP ${res.statusCode} from ${currentUrl}`));
        }
        let data = '';
        res.setEncoding('utf8');
        res.on('data', (chunk) => { data += chunk; });
        res.on('end', () => resolve(data));
      }).on('error', reject);
    }
    get(url, 0);
  });
}

/**
 * Computes sha256 of a local file.
 */
function computeFileSha256(filePath) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash('sha256');
    const stream = fs.createReadStream(filePath);
    stream.on('data', (chunk) => hash.update(chunk));
    stream.on('end', () => resolve(hash.digest('hex')));
    stream.on('error', reject);
  });
}

/**
 * Formats bytes into a human-readable string.
 */
function formatBytes(bytes) {
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * Ensures the target binary is downloaded, extracted, verified and ready to run.
 */
async function ensureBinary(version) {
  // Allow explicit override via environment
  if (process.env.PIGASUS_BINARY_PATH && fs.existsSync(process.env.PIGASUS_BINARY_PATH)) {
    return process.env.PIGASUS_BINARY_PATH;
  }

  const pkg = require('../package.json');
  const binaryVersion = pkg.pigasusVersion || (version ? version.replace(/[-+].*$/, '') : '1.4.6');

  const platformInfo = getPlatformInfo();
  const cacheDir = getCacheDir();
  const binaryFileName = `pigasus-${binaryVersion}-${platformInfo.os}-${platformInfo.arch}`;
  const targetBinaryPath = path.join(cacheDir, binaryFileName);

  // If cached and executable, return immediately
  if (fs.existsSync(targetBinaryPath)) {
    try {
      fs.accessSync(targetBinaryPath, fs.constants.X_OK);
      return targetBinaryPath;
    } catch (_) {
      try {
        fs.chmodSync(targetBinaryPath, 0o755);
        return targetBinaryPath;
      } catch (__) {
        // Fall through to re-download
      }
    }
  }

  // Ensure cache directory exists
  fs.mkdirSync(cacheDir, { recursive: true });

  const archiveName = `pigasus_v${binaryVersion}_${platformInfo.archiveSuffix}`;
  let baseDownloadUrl = process.env.PIGASUS_DOWNLOAD_URL ||
    `https://github.com/${GITHUB_REPO}/releases/download/v${binaryVersion}`;

  if (process.env.GITHUB_MIRROR) {
    baseDownloadUrl = baseDownloadUrl.replace('https://github.com/', process.env.GITHUB_MIRROR.replace(/\/$/, '') + '/');
  }

  const archiveUrl = `${baseDownloadUrl}/${archiveName}`;
  const checksumsUrl = `${baseDownloadUrl}/checksums.txt`;

  const tmpArchive = path.join(cacheDir, `${archiveName}.tmp-${Date.now()}`);
  const tmpExtractDir = path.join(cacheDir, `extract-${Date.now()}`);

  const isTTY = process.stderr.isTTY;
  if (isTTY) {
    process.stderr.write(`Downloading Pigasus v${binaryVersion} for ${platformInfo.os}_${platformInfo.arch}...\n`);
  }

  const destStream = fs.createWriteStream(tmpArchive);

  try {
    let lastPercent = -1;
    await downloadFile(archiveUrl, destStream, (current, total) => {
      if (!isTTY) return;
      const percent = Math.floor((current / total) * 100);
      if (percent !== lastPercent) {
        lastPercent = percent;
        const barWidth = 24;
        const filled = Math.round((barWidth * percent) / 100);
        const bar = '='.repeat(filled) + (filled < barWidth ? '>' : '') + ' '.repeat(Math.max(0, barWidth - filled - 1));
        process.stderr.write(`\r[${bar}] ${percent}% (${formatBytes(current)} / ${formatBytes(total)})`);
      }
    });

    if (isTTY) {
      process.stderr.write('\n');
    }

    // Verify Checksum if checksums.txt is available
    try {
      const checksumsText = await fetchText(checksumsUrl);
      const expectedChecksum = checksumsText
        .split('\n')
        .map((line) => line.trim().split(/\s+/))
        .find((parts) => parts.length >= 2 && parts[1].includes(archiveName));

      if (expectedChecksum) {
        const actualSha256 = await computeFileSha256(tmpArchive);
        if (actualSha256.toLowerCase() !== expectedChecksum[0].toLowerCase()) {
          throw new Error(
            `SHA-256 checksum mismatch for ${archiveName}!\nExpected: ${expectedChecksum[0]}\nActual:   ${actualSha256}`
          );
        }
      }
    } catch (csErr) {
      if (csErr.message.includes('checksum mismatch')) {
        throw csErr;
      }
    }

    // Extract using system tar
    fs.mkdirSync(tmpExtractDir, { recursive: true });
    await new Promise((resolve, reject) => {
      execFile('tar', ['-xzf', tmpArchive, '-C', tmpExtractDir], (err) => {
        if (err) return reject(new Error(`Failed to extract tar archive: ${err.message}`));
        resolve();
      });
    });

    // Locate binary in extracted dir (supports flat or nested release archives)
    const extractedBinary = findBinary(tmpExtractDir, platformInfo.binaryName);
    if (!extractedBinary) {
      throw new Error(`Extracted archive did not contain binary '${platformInfo.binaryName}'`);
    }

    // Move to final target path atomically
    fs.chmodSync(extractedBinary, 0o755);
    fs.renameSync(extractedBinary, targetBinaryPath);

    return targetBinaryPath;
  } finally {
    // Cleanup temporary files
    try { if (fs.existsSync(tmpArchive)) fs.unlinkSync(tmpArchive); } catch (_) {}
    try { if (fs.existsSync(tmpExtractDir)) fs.rmSync(tmpExtractDir, { recursive: true, force: true }); } catch (_) {}
  }
}

module.exports = {
  getCacheDir,
  ensureBinary,
  findBinary,
};
