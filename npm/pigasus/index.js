'use strict';

const os = require('os');
const { spawn } = require('child_process');
const { ensureBinary, getCacheDir } = require('./lib/download');
const { getPlatformInfo } = require('./lib/platform');
const pkg = require('./package.json');

/**
 * Returns the path to the cached or downloaded Pigasus binary.
 *
 * @param {string} [version] - Optional version override, defaults to package.json version.
 * @returns {Promise<string>} Path to executable.
 */
async function getBinaryPath(version = pkg.version) {
  return await ensureBinary(version);
}

/**
 * Runs the Pigasus binary with the provided CLI arguments and options.
 *
 * @param {string[]} [args] - CLI arguments to pass to pigasus.
 * @param {import('child_process').SpawnOptions} [options] - Node spawn options.
 * @returns {Promise<number>} Exit code of the process.
 */
async function run(args = process.argv.slice(2), options = {}) {
  const binaryPath = await getBinaryPath();
  const spawnOptions = {
    stdio: 'inherit',
    ...options,
  };

  const child = spawn(binaryPath, args, spawnOptions);

  // Forward terminal resize signals (crucial for Bubbletea TUI parity)
  const onResize = () => {
    if (child.pid) {
      try { process.kill(child.pid, 'SIGWINCH'); } catch (_) {}
    }
  };
  process.on('SIGWINCH', onResize);

  return new Promise((resolve) => {
    child.on('exit', (code, signal) => {
      process.removeListener('SIGWINCH', onResize);
      if (signal) {
        resolve(128 + (os.constants.signals[signal] || 1));
      } else {
        resolve(code !== null ? code : 0);
      }
    });
  });
}

module.exports = {
  getBinaryPath,
  run,
  getCacheDir,
  getPlatformInfo,
  version: pkg.version,
};
