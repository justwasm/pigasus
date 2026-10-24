'use strict';

const os = require('os');

/**
 * Maps the current Node environment to Pigasus release platform and arch strings.
 *
 * Supported release artifacts:
 * - Darwin_arm64
 * - Darwin_x86_64
 * - Linux_arm64
 * - Linux_x86_64
 */
function getPlatformInfo() {
  const platform = os.platform();
  const arch = os.arch();

  let osName = '';
  if (platform === 'darwin') {
    osName = 'Darwin';
  } else if (platform === 'linux') {
    osName = 'Linux';
  } else if (platform === 'win32') {
    throw new Error(
      'Pigasus currently does not provide native Windows binaries. ' +
      'Please run Pigasus inside WSL2 (Windows Subsystem for Linux).'
    );
  } else {
    throw new Error(`Unsupported operating system: ${platform}`);
  }

  let archName = '';
  if (arch === 'arm64' || arch === 'aarch64') {
    archName = 'arm64';
  } else if (arch === 'x64' || arch === 'x86_64') {
    archName = 'x86_64';
  } else {
    throw new Error(`Unsupported CPU architecture: ${arch}`);
  }

  const binaryName = 'pigasus';
  const archiveSuffix = `${osName}_${archName}.tar.gz`;

  return {
    os: osName,
    arch: archName,
    binaryName,
    archiveSuffix,
  };
}

module.exports = {
  getPlatformInfo,
};
