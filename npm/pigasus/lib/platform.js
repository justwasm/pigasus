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
 * - Android_arm64
 * - Windows_x86_64
 * - Windows_arm64
 */
function getPlatformInfo() {
  const platform = os.platform();
  const arch = os.arch();

  let osName = '';
  let ext = 'tar.gz';
  let binaryName = 'pigasus';

  if (platform === 'darwin') {
    osName = 'Darwin';
  } else if (platform === 'linux') {
    osName = 'Linux';
  } else if (platform === 'android') {
    osName = 'Android';
  } else if (platform === 'win32') {
    osName = 'Windows';
    ext = 'zip';
    binaryName = 'pigasus.exe';
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

  const archiveSuffix = `${osName}_${archName}.${ext}`;

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
